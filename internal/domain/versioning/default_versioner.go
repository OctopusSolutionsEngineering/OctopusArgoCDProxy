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

type DefaultVersioner struct {
}

// GenerateReleaseVersion will use the target revision, then a matching image version, then a git sha, then just a timestamp
// to generate the release version.
func (o *DefaultVersioner) GenerateReleaseVersion(octo octopus.OctopusClient, project models.ArgoCDProject, updateMessage models.ApplicationUpdateMessage) (string, error) {
	timestamp := time.Now().Format("20060102150405")

	sha := strings.TrimSpace(updateMessage.CommitSha)
	shaSuffix := ""
	if sha != "" {
		if len(sha) > 12 {
			sha = sha[:11]
		}
		shaSuffix = "-" + sha
	}

	// the target revision is a useful version
	if len(Semver.FindStringSubmatch(updateMessage.TargetRevision)) != 0 {
		return updateMessage.TargetRevision + "-" + timestamp, nil
	}

	// There is an image version we want to use
	if project.ReleaseVersionImage != "" && updateMessage.Images != nil {
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
				return v1.Compare(v2) > 0
			}

			if err1 == nil {
				return true
			}

			return versions[a] > versions[b]
		})

		if len(versions) != 0 {
			return versions[0] + "-" + timestamp + shaSuffix, nil
		}
	}

	// There is a SHA, add it
	if shaSuffix != "" {
		return timestamp + shaSuffix, nil
	}

	// if all else fails, use a date ver
	return time.Now().Format("2006.01.02.150405"), nil
}
