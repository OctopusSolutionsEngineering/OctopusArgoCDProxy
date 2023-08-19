package octopus_apis

import (
	"github.com/OctopusDeploy/go-octopusdeploy/octopusdeploy"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/types"
)

type OctopusClient interface {
	// GetProjects returns the details of projects that match the incoming message
	GetProjects(updateMessage models.ApplicationUpdateMessage) ([]models.ArgoCDProjectExpanded, error)
	// CreateAndDeployRelease will ensure the release is deployed to the correct environment, creating a new release if necessary
	CreateAndDeployRelease(project models.ArgoCDProjectExpanded, updateMessage models.ApplicationUpdateMessage, version types.OctopusReleaseVersion) error
	// GetReleaseVersions returns the releases associated with a project
	GetReleaseVersions(project *octopusdeploy.Project) ([]types.OctopusReleaseVersion, error)
	// IsDeployed returns true if the release is deployed to the specified environment
	IsDeployed(project *octopusdeploy.Project, releaseVersion types.OctopusReleaseVersion, environment *octopusdeploy.Environment) (bool, error)
}
