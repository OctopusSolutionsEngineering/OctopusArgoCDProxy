package hanlders

import (
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
)

type CreateReleaseHandler struct {
	logger    apploggers.AppLogger
	octo      octopus_apis.OctopusClient
	argo      *argocd_apis.ArgoCDClient
	versioner versioners.ReleaseVersioner
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
		logger:    logger,
		octo:      octo,
		argo:      argocdClient,
		versioner: &versioners.DefaultVersioner{},
	}, nil
}

// CreateRelease will attempt to create a release for up to two hours, which takes the standard maintenance window
// of a cloud hosted instanced into account.
func (c CreateReleaseHandler) CreateRelease(applicationUpdateMessage models.ApplicationUpdateMessage) error {
	err := retry.Do(
		func() error {
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
				version, err := c.versioner.GenerateReleaseVersion(project, applicationUpdateMessage)

				if err != nil {
					return err
				}

				err = c.octo.CreateAndDeployRelease(project, applicationUpdateMessage, version)

				if err != nil {
					return err
				}
			}

			return nil
		}, retry_config.HandlerRetryOptions...)

	return err
}

func (c CreateReleaseHandler) getImages(applicationUpdateMessage models.ApplicationUpdateMessage) ([]string, error) {
	tree, err := c.argo.GetApplicationResourceTree(applicationUpdateMessage.Application, applicationUpdateMessage.Namespace)

	if err != nil {
		return nil, err
	}

	images := lo.FlatMap(tree.Nodes, func(item v1alpha1.ResourceNode, index int) []string {
		return item.Images
	})

	return lo.Uniq(images), nil
}
