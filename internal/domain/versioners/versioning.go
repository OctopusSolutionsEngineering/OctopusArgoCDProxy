package versioners

import (
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
)

// ReleaseVersioner defines the functions required to create an Octoipus release version
type ReleaseVersioner interface {
	GenerateReleaseVersion(project models.ArgoCDProjectExpanded, updateMessage models.ApplicationUpdateMessage) (string, error)
}
