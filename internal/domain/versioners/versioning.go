package versioners

import (
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/octopus_apis"
)

// ReleaseVersioner defines the functions required to create an Octoipus release version
type ReleaseVersioner interface {
	GenerateReleaseVersion(octo octopus_apis.OctopusClient, project models.ArgoCDProjectExpanded, updateMessage models.ApplicationUpdateMessage) (string, error)
}
