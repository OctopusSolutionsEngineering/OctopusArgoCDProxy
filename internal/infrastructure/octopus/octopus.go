package octopus

import "github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain"

type OctopusClient interface {
	CreateAndDeployRelease(pdateMessage domain.ApplicationUpdateMessage) error
}
