package versioning

import (
	"fmt"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/octopus"
	"github.com/samber/lo"
	"strings"
	"time"
)

type SimpleVersioner struct {
}

// GenerateReleaseVersion will use the target revision, then a matching image version, then a git sha. It uses semver metadata
// to ensure release versions are unique.
func (o *SimpleVersioner) GenerateReleaseVersion(octo octopus.OctopusClient, project models.ArgoCDProject, updateMessage models.ApplicationUpdateMessage) string {

	fallbackVersion := time.Now().Format("2006.01.02.150405")

	releases, err := octo.GetReleaseVersions(project.Project.ID)

	if err != nil {
		return fallbackVersion
	}

	// the target revision is a useful version
	if len(Semver.FindStringSubmatch(updateMessage.TargetRevision)) != 0 {
		existingVersions := lo.Filter(releases, func(item string, index int) bool {
			return item == updateMessage.TargetRevision
		})

		if len(existingVersions) == 0 {
			return updateMessage.TargetRevision
		}

		return updateMessage.TargetRevision + "+deployment" + fmt.Sprint(len(existingVersions)+1)
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

		if len(versions) != 0 {
			existingVersions := lo.Filter(releases, func(item string, index int) bool {
				return item == versions[0]
			})

			if len(existingVersions) == 0 {
				return versions[0]
			}

			return versions[0] + "+deployment" + fmt.Sprint(len(existingVersions)+1)
		}
	}

	// if all else fails, use a date ver
	return fallbackVersion
}
