package versioners

import (
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/types"
)

// ReleaseVersioner defines the functions required to create an Octopus release version
type ReleaseVersioner interface {
	GenerateReleaseVersion(project models.ArgoCDProjectExpanded, updateMessage models.ApplicationUpdateMessage) (types.OctopusReleaseVersion, error)
}
