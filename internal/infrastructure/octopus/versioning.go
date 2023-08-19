package octopus

import (
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
)

// ReleaseVersioner defines the functions required to create an Octoipus release version
type ReleaseVersioner interface {
	GenerateReleaseVersion(octo OctopusClient, project models.ArgoCDProjectExpanded, updateMessage models.ApplicationUpdateMessage) (string, error)
}
