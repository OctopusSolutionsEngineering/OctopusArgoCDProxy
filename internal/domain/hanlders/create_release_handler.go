package hanlders

import (
	"errors"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/versioners"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/apploggers"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/argocd_apis"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/octopus_apis"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/retry_config"
	"github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/avast/retry-go"
	"github.com/samber/lo"
	"strings"
	"sync"
	"time"
)

type CreateReleaseHandler struct {
	logger          apploggers.AppLogger
	octo            octopus_apis.OctopusClient
	argo            *argocd_apis.ArgoCDClient
	versioner       versioners.ReleaseVersioner
	projectReleases sync.Map
}

func NewCreateReleaseHandler() (*CreateReleaseHandler, error) {
	logger, err := apploggers.NewDevProdLogger()

	if err != nil {
		return nil, err
	}

	octo, err := octopus_apis.NewLiveOctopusClient()

	if err != nil {
		return nil, err
	}

	argocdClient, err := argocd_apis.NewClient()

	if err != nil {
		return nil, err
	}

	return &CreateReleaseHandler{
		logger:          logger,
		octo:            octo,
		argo:            argocdClient,
		versioner:       &versioners.SimpleRedeploymentVersioner{},
		projectReleases: sync.Map{},
	}, nil
}

// CreateRelease will attempt to create a release for up to two hours, which takes the standard maintenance window
// of a cloud hosted instanced into account.
func (c CreateReleaseHandler) CreateRelease(applicationUpdateMessage models.ApplicationUpdateMessage) error {

	images, err := c.getImages(applicationUpdateMessage)

	// We can gracefully fall back if the connection back to argo failed
	if err == nil {
		applicationUpdateMessage.Images = images
	} else {
		applicationUpdateMessage.Images = []string{}
		c.logger.GetLogger().Error("octoargosync-init-argoappimages: Failed to get the list of images from Argo CD. " +
			"Verify the ARGOCD_SERVER and ARGOCD_TOKEN environment variables are valid. " +
			"The Octopus release version will not use any image version. " + err.Error())
	}

	c.logger.GetLogger().Info("Received message from " + applicationUpdateMessage.Application + " in namespace " +
		applicationUpdateMessage.Namespace + " for SHA " + applicationUpdateMessage.CommitSha + " and release version " +
		applicationUpdateMessage.TargetRevision + " which includes the images " + strings.Join(applicationUpdateMessage.Images, ","))

	expandedProjects, err := c.octo.GetProjects(applicationUpdateMessage)

	if err != nil {
		return err
	}

	if len(expandedProjects) == 0 {
		c.logger.GetLogger().Info("No projects found configured for " + applicationUpdateMessage.Application + " in namespace " + applicationUpdateMessage.Namespace)
		c.logger.GetLogger().Info("To create releases for this application, add the Metadata.ArgoCD.Application[" +
			applicationUpdateMessage.Namespace + "/" + applicationUpdateMessage.Application + "].EnvironmentName variable with a value matching the application's environment name, like \"Development\"")
	}

	for _, project := range expandedProjects {
		added := time.Now()
		c.projectReleases.Store(project.Project.ID, added)
		go func(project models.ArgoCDProjectExpanded, projectReleases *sync.Map, added time.Time) {
			err := retry.Do(
				func() error {
					// Check to see if another release was created after this one. In this case we drop the old release
					// assuming the newer one is what should be passed to Octopus. This can happen if multiple
					// releases were in a retry loop, a new release is added just as Octopus come back online,
					// meaning we drop the old releases.
					if lastAdded, exists := projectReleases.Load(project.Project.ID); exists {
						if lastAddedTime, ok := lastAdded.(time.Time); ok {
							if lastAddedTime.After(added) {
								return nil
							}
						}
					}

					// The other edge case we want to catch is if another instance of the proxy has created a release
					// after this release was first supposed to be created. If so, we drop this release as it is
					// old now and should not appear to be the latest deployment.
					lastestRelease, err := c.octo.GetLatestDeploymentRelease(project.Project, project.Environment)

					if err != nil {
						return err
					}

					if lastestRelease != nil && lastestRelease.Assembled.After(added) {
						return nil
					}

					// It is conceivable that other race conditions can occur. Multiple proxies receiving many
					// requests to create a release for a project on an Octopus instance that is not responding
					// might lead to multiple releases being created in the wrong order. However, that scenario
					// assumes many releases happening in quick succession, and in such an environment, the
					// Octopus dashboard will soon correct itself again. So we don't try to enforce any strict
					// synchronisation between proxies, and rely on the fact that releases will eventually be
					// consistent.

					version, err := c.versioner.GenerateReleaseVersion(project, applicationUpdateMessage)

					if err != nil {
						return err
					}

					return c.octo.CreateAndDeployRelease(project, applicationUpdateMessage, version)
				}, retry_config.HandlerRetryOptions...)

			// We really, really tried to create the release, but there is nothing left to do but print an error.
			if err != nil {
				c.logger.GetLogger().Error("octoargosync-release-failed: Failed to create a release: " + err.Error())
			}
		}(project, &c.projectReleases, added)
	}

	return nil
}

func (c CreateReleaseHandler) getImages(applicationUpdateMessage models.ApplicationUpdateMessage) ([]string, error) {
	if c.argo == nil {
		return nil, errors.New("the agro client is nil")
	}

	tree, err := c.argo.GetApplicationResourceTree(applicationUpdateMessage.Application, applicationUpdateMessage.Namespace)

	if err != nil {
		return nil, err
	}

	images := lo.FlatMap(tree.Nodes, func(item v1alpha1.ResourceNode, index int) []string {
		return item.Images
	})

	return lo.Uniq(images), nil
}
