package hanlders

import (
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/versioning"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/argocd"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/logging"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/octopus"
	"github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/samber/lo"
	"strings"
)

type CreateReleaseHandler struct {
	logger    logging.AppLogger
	octo      octopus.OctopusClient
	argo      *argocd.Client
	versioner versioning.ReleaseVersioner
}

func NewCreateReleaseHandler() (*CreateReleaseHandler, error) {
	logger, err := logging.NewDevProdLogger()

	if err != nil {
		return nil, err
	}

	octo, err := octopus.NewLiveOctopusClient(&versioning.DefaultVersioner{})

	if err != nil {
		return nil, err
	}

	argocdClient, err := argocd.NewClient()

	if err != nil {
		return nil, err
	}

	return &CreateReleaseHandler{
		logger:    logger,
		octo:      octo,
		argo:      argocdClient,
		versioner: &versioning.DefaultVersioner{},
	}, nil
}

func (c CreateReleaseHandler) CreateRelease(applicationUpdateMessage models.ApplicationUpdateMessage) error {

	images, err := c.getImages(applicationUpdateMessage)

	// We can gracefully fall back if the connection back to argo failed
	if err == nil {
		applicationUpdateMessage.Images = images
	} else {
		applicationUpdateMessage.Images = []string{}
		c.logger.GetLogger().Error("Failed to get the list of images from Argo CD. " +
			"The Octopus release version will not use any image version. " + err.Error())
	}

	c.logger.GetLogger().Info("Received message from " + applicationUpdateMessage.Application + " in namespace " +
		applicationUpdateMessage.Namespace + " for sha " + applicationUpdateMessage.CommitSha + " which includes the images " +
		strings.Join(applicationUpdateMessage.Images, ","))

	err = c.octo.CreateAndDeployRelease(applicationUpdateMessage)

	if err != nil {
		return err
	}

	return nil
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
