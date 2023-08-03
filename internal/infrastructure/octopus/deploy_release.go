package octopus

import (
	"errors"
	"github.com/OctopusDeploy/go-octopusdeploy/octopusdeploy"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/logging"
	"github.com/samber/lo"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const MaxInt = 2147483647

var ApplicationEnvironmentVariable = regexp.MustCompile("^ArgoCD\\.Application\\[([^\\[\\]]*?)]\\.Environment$")
var ApplicationImageVersionVariable = regexp.MustCompile("^ArgoCD\\.Application\\[([^\\[\\]]*?)]\\.ImageForReleaseVersion$")
var Semver = regexp.MustCompile("^(?P<major>0|[1-9]\\d*)\\.(?P<minor>0|[1-9]\\d*)\\.(?P<patch>0|[1-9]\\d*)(?:-(?P<prerelease>(?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\\.(?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\\+(?P<buildmetadata>[0-9a-zA-Z-]+(?:\\.[0-9a-zA-Z-]+)*))?$")

type ArgoCDProject struct {
	Project             *octopusdeploy.Project
	Environment         string
	ReleaseVersionImage string
}

type LiveOctopusClient struct {
	client *octopusdeploy.Client
	logger logging.AppLogger
}

func NewLiveOctopusClient() (*LiveOctopusClient, error) {
	client, err := getClient()

	if err != nil {
		return nil, err
	}

	logger, err := logging.NewDevProdLogger()

	if err != nil {
		return nil, err
	}

	return &LiveOctopusClient{
		client: client,
		logger: logger,
	}, nil
}

func (o *LiveOctopusClient) CreateAndDeployRelease(updateMessage domain.ApplicationUpdateMessage) error {
	projects, err := o.getProject(updateMessage.Application, updateMessage.Namespace)

	if err != nil {
		return err
	}

	for _, project := range projects {

		defaultChannel, err := o.getDefaultChannel(project.Project)

		if err != nil {
			return err
		}

		version := o.generateReleaseVersion(project, updateMessage)

		release := octopusdeploy.NewRelease(defaultChannel.ID, project.Project.ID, version)
		release, err = o.client.Releases.Add(release)

		if err != nil {
			return err
		}

		environmentId, err := o.getEnvironmentId(project.Environment)

		if err != nil {
			return err
		}

		deployment := octopusdeploy.NewDeployment(environmentId, release.ID)
		deployment, err = o.client.Deployments.Add(deployment)

		if err != nil {
			return err
		}

		o.logger.GetLogger().Info("Created release " + release.ID + " and deployment " + deployment.ID +
			" in environment " + project.Environment + " for project " + project.Project.Name)

	}

	return nil
}

func getClient() (*octopusdeploy.Client, error) {
	if os.Getenv("OCTOPUS_SERVER") == "" {
		return nil, errors.New("OCTOPUS_SERVER must be defined")
	}

	if os.Getenv("OCTOPUS_API_KEY") == "" {
		return nil, errors.New("OCTOPUS_API_KEY must be defined")
	}

	octopusUrl, err := url.Parse(os.Getenv("OCTOPUS_SERVER"))

	if err != nil {
		return nil, err
	}

	return octopusdeploy.NewClient(nil, octopusUrl, os.Getenv("OCTOPUS_API_KEY"), os.Getenv("OCTOPUS_SPACE_ID"))
}

// getProject scans Octopus for the project that has been linked to the Argo CD Application and namespace
func (o *LiveOctopusClient) getProject(application string, namespace string) ([]ArgoCDProject, error) {
	projects, err := o.client.Projects.Get(octopusdeploy.ProjectsQuery{Take: MaxInt})

	if err != nil {
		return nil, err
	}

	matchingProjects := lo.FilterMap(projects.Items, func(project *octopusdeploy.Project, index int) (ArgoCDProject, bool) {
		variables, err := o.client.Variables.GetAll(project.ID)

		if err != nil {
			return ArgoCDProject{}, false
		}

		appNameEnvironments := lo.FilterMap(variables.Variables, func(variable *octopusdeploy.Variable, index int) (string, bool) {
			match := ApplicationEnvironmentVariable.FindStringSubmatch(variable.Name)

			if len(match) != 2 || match[1] != namespace+"/"+application {
				return "", false
			}

			return variable.Value, true
		})

		releaseVersionImages := lo.FilterMap(variables.Variables, func(variable *octopusdeploy.Variable, index int) (string, bool) {
			match := ApplicationImageVersionVariable.FindStringSubmatch(variable.Name)

			if len(match) != 2 || match[1] != namespace+"/"+application {
				return "", false
			}

			return variable.Value, true
		})

		releaseVersionImage := ""
		if len(releaseVersionImages) != 0 {
			releaseVersionImage = releaseVersionImages[0]
		}

		if len(appNameEnvironments) != 0 {
			return ArgoCDProject{
				Project:             project,
				Environment:         appNameEnvironments[0],
				ReleaseVersionImage: releaseVersionImage,
			}, true
		}

		return ArgoCDProject{}, false
	})

	return matchingProjects, nil
}

func (o *LiveOctopusClient) getDefaultChannel(project *octopusdeploy.Project) (*octopusdeploy.Channel, error) {
	channelQuery := octopusdeploy.ChannelsQuery{
		Take: MaxInt,
		Skip: 0,
	}

	channels, err := o.client.Channels.Get(channelQuery)

	if err != nil {
		return nil, err
	}

	defaultChannel := lo.Filter(channels.Items, func(item *octopusdeploy.Channel, index int) bool {
		return item.IsDefault && item.ProjectID == project.ID
	})

	if len(defaultChannel) == 1 {
		return defaultChannel[0], nil
	}

	return nil, errors.New("could not find the default channel")
}

func (o *LiveOctopusClient) getEnvironmentId(environment string) (string, error) {
	if strings.HasPrefix("Environments-", environment) {
		return environment, nil
	}

	environmentsQuery := octopusdeploy.EnvironmentsQuery{
		Name: environment,
	}

	environments, err := o.client.Environments.Get(environmentsQuery)

	if err != nil {
		return "", nil
	}

	filteredEnvironments := lo.Filter(environments.Items, func(e *octopusdeploy.Environment, index int) bool {
		return e.Name == environment
	})

	if len(filteredEnvironments) == 1 {
		return filteredEnvironments[0].ID, nil
	}

	return "", errors.New("failed to find an environment called " + environment)
}

func (o *LiveOctopusClient) generateReleaseVersion(project ArgoCDProject, updateMessage domain.ApplicationUpdateMessage) string {
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
