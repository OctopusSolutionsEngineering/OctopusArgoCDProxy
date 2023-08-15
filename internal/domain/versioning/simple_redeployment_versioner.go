package versioning

import (
	"github.com/Masterminds/semver/v3"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/octopus"
	"github.com/samber/lo"
	"sort"
	"strings"
	"time"
)

type SimpleRedeploymentVersioner struct {
}

// GenerateReleaseVersion extracts the version from the target revision or the image version. It pays no attention
// to existing releases, meaning redeployments from Argo trigger redeployemnts in Octopus.
func (o *SimpleRedeploymentVersioner) GenerateReleaseVersion(octo octopus.OctopusClient, project models.ArgoCDProject, updateMessage models.ApplicationUpdateMessage) (string, error) {

	fallbackVersion := time.Now().Format("2006.01.02.150405")

	// the target revision is a useful version
	_, err := semver.NewVersion(updateMessage.TargetRevision)
	if err == nil {
		return updateMessage.TargetRevision, nil
	}

	// There is an image version we want to use
	if project.ReleaseVersionImage != "" {
		versions := lo.FilterMap(updateMessage.Images, func(item string, index int) (string, bool) {
			split := strings.Split(item, ":")
			if len(split) == 2 && split[0] == project.ReleaseVersionImage {
				return split[1], true
			}

			return "", false
		})

		sort.SliceStable(versions, func(a, b int) bool {
			v1, err1 := semver.NewVersion(versions[a])
			v2, err2 := semver.NewVersion(versions[b])

			if err1 == nil && err2 == nil {
				return v1.Compare(v2) < 0
			}

			if err1 == nil {
				return false
			}

			return versions[a] < versions[b]
		})

		if len(versions) != 0 {

			return versions[0], nil
		}
	}

	// if all else fails, use a date ver
	return fallbackVersion, nil
}
