package versioning

import (
	"fmt"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/octopus"
	"github.com/samber/lo"
	"k8s.io/utils/strings/slices"
	"strings"
	"time"
)

type SimpleVersioner struct {
}

// GenerateReleaseVersion will use the target revision, then a matching image version, then a git sha. It uses semver metadata
// to ensure release versions are unique.
func (o *SimpleVersioner) GenerateReleaseVersion(octo octopus.OctopusClient, project models.ArgoCDProject, updateMessage models.ApplicationUpdateMessage) (string, error) {

	fallbackVersion := time.Now().Format("2006.01.02.150405")

	releases, err := octo.GetReleaseVersions(project.Project.ID)

	if err != nil {
		return "", err
	}

	// the target revision is a useful version
	if len(Semver.FindStringSubmatch(updateMessage.TargetRevision)) != 0 {
		version := updateMessage.TargetRevision

		isDeployed, err := octo.IsDeployed(project.Project.ID, version, project.Environment)

		if err != nil {
			return "", err
		}

		if !isDeployed {
			return version, nil
		}

		if slices.Index(releases, version) == -1 {
			return updateMessage.TargetRevision, nil
		}

		for count := 2; count < 1000; count++ {
			thisVersion := version + "+deployment" + fmt.Sprint(count)
			if slices.Index(releases, thisVersion) == -1 {
				return thisVersion, nil
			}
		}

		return time.Now().Format("20060102150405"), nil
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

			version := versions[0]

			isDeployed, err := octo.IsDeployed(project.Project.ID, version, project.Environment)

			if err != nil {
				return "", err
			}

			if !isDeployed {
				return version, nil
			}

			for count := 2; count < 1000; count++ {
				thisVersion := version + "+deployment" + fmt.Sprint(count)
				if slices.Index(releases, thisVersion) == -1 {
					return thisVersion, nil
				}
			}

			return time.Now().Format("20060102150405"), nil
		}
	}

	// if all else fails, use a date ver
	return fallbackVersion, nil
}
