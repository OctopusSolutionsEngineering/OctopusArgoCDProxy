package octopus_apis

import (
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
)

type OctopusClient interface {
	GetProjects(updateMessage models.ApplicationUpdateMessage) ([]models.ArgoCDProjectExpanded, error)
	// CreateAndDeployRelease will ensure the release is deployed to the correct environment, creating a new release if necessary
	CreateAndDeployRelease(project models.ArgoCDProjectExpanded, updateMessage models.ApplicationUpdateMessage, version string) error
	// GetReleaseVersions returns the releases associated with a project
	GetReleaseVersions(projectId string) ([]string, error)
	// IsDeployed returns true if the release is deployed to the specified environment
	IsDeployed(projectId string, releaseVersion string, environmentName string) (bool, error)
}
