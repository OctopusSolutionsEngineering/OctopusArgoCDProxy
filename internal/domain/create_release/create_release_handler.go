package create_release

import (
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/json"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/versioning"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/argocd"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/logging"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/octopus"
	"github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/samber/lo"
	"io"
	"strings"
)

type CreateReleaseHandler struct {
	logger    logging.AppLogger
	extractor *json.BodyExtractor
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
		extractor: &json.BodyExtractor{},
		octo:      octo,
		argo:      argocdClient,
		versioner: &versioning.DefaultVersioner{},
	}, nil
}

func (c CreateReleaseHandler) CreateRelease(reader *io.ReadCloser) error {
	applicationUpdateMessage := models.ApplicationUpdateMessage{}
	err := c.extractor.DeserializeJson(*reader, &applicationUpdateMessage)

	if err != nil {
		return err
	}

	tree, err := c.argo.GetApplicationResourceTree(applicationUpdateMessage.Application, applicationUpdateMessage.Namespace)

	if err != nil {
		return err
	}

	images := lo.FlatMap(tree.Nodes, func(item v1alpha1.ResourceNode, index int) []string {
		return item.Images
	})

	applicationUpdateMessage.Images = lo.Uniq(images)

	c.logger.GetLogger().Info("Received message from " + applicationUpdateMessage.Application + " in namespace " +
		applicationUpdateMessage.Namespace + " which includes the images " +
		strings.Join(applicationUpdateMessage.Images, ","))

	err = c.octo.CreateAndDeployRelease(applicationUpdateMessage)

	if err != nil {
		return err
	}

	return nil
}
