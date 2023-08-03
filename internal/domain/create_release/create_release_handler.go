package create_release

import (
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/json"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/logging"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/octopus"
	"io"
)

type CreateReleaseHandler struct {
	logger    logging.AppLogger
	extractor *json.BodyExtractor
	octo      octopus.OctopusClient
}

func NewCreateReleaseHandler() (*CreateReleaseHandler, error) {
	logger, err := logging.NewDevProdLogger()

	if err != nil {
		return nil, err
	}

	octo, err := octopus.NewLiveOctopusClient()

	if err != nil {
		return nil, err
	}

	extractor := &json.BodyExtractor{}

	return &CreateReleaseHandler{
		logger:    logger,
		extractor: extractor,
		octo:      octo,
	}, nil
}

func (c CreateReleaseHandler) CreateRelease(reader *io.ReadCloser) error {
	applicationUpdateMessage := domain.ApplicationUpdateMessage{}
	err := c.extractor.DeserializeJson(*reader, &applicationUpdateMessage)

	if err != nil {
		return err
	}

	err = c.octo.CreateAndDeployRelease(applicationUpdateMessage)

	if err != nil {
		return err
	}

	return nil
}
