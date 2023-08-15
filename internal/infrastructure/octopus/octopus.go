package octopus

import (
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
)

type OctopusClient interface {
	CreateAndDeployRelease(updateMessage models.ApplicationUpdateMessage) error
	GetReleaseVersions(projectId string) ([]string, error)
}
