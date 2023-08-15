package versioning

import (
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/octopus"
	"github.com/samber/lo"
	"strings"
	"time"
)

type DefaultVersioner struct {
}

// GenerateReleaseVersion will use the target revision, then a matching image version, then a git sha, then just a timestamp
// to generate the release version.
func (o *DefaultVersioner) GenerateReleaseVersion(octo octopus.OctopusClient, project models.ArgoCDProject, updateMessage models.ApplicationUpdateMessage) string {
	timestamp := time.Now().Format("20060102150405")

	sha := ""
	shaSuffix := ""
	if updateMessage.CommitSha != "" {
		sha = strings.TrimSpace(updateMessage.CommitSha[0:12])
		shaSuffix = "-" + sha
	}

	// the target revision is a useful version
	if len(Semver.FindStringSubmatch(updateMessage.TargetRevision)) != 0 {
		return updateMessage.TargetRevision + "-" + timestamp + shaSuffix
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
			return versions[0] + "-" + timestamp + shaSuffix
		}
	}

	// There is a SHA
	if shaSuffix != "" {
		return timestamp + shaSuffix
	}

	// if all else fails, use a date ver
	return time.Now().Format("2006.01.02.150405")
}
