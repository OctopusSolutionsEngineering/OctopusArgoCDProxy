package octopus

import (
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
)

type OctopusClient interface {
	// CreateAndDeployRelease will ensure the release is deployed to the correct environment, creating a new release if necessary
	CreateAndDeployRelease(updateMessage models.ApplicationUpdateMessage) error
	// GetReleaseVersions returns the releases associated with a project
	GetReleaseVersions(projectId string) ([]string, error)
	// IsDeployed returns true if the release is deployed to the specified environment
	IsDeployed(projectId string, releaseVersion string, environmentName string) (bool, error)
}
