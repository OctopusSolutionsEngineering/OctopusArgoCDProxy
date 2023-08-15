package octopus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/OctopusDeploy/go-octopusdeploy/octopusdeploy"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/logging"
	"github.com/allegro/bigcache/v3"
	"github.com/samber/lo"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const MaxInt = 2147483647

var ApplicationEnvironmentVariable = regexp.MustCompile("^Metadata.ArgoCD\\.Application\\[([^\\[\\]]*?)]\\.Environment$")
var ApplicationImageVersionVariable = regexp.MustCompile("^Metadata.ArgoCD\\.Application\\[([^\\[\\]]*?)]\\.ImageForReleaseVersion$")

// LiveOctopusClient interacts with a live Octopus API endpoint, and implements caching to reduce network calls.
type LiveOctopusClient struct {
	client    *octopusdeploy.Client
	logger    logging.AppLogger
	versioner ReleaseVersioner
	bigCache  *bigcache.BigCache
}

func NewLiveOctopusClient(versioner ReleaseVersioner) (*LiveOctopusClient, error) {
	client, err := getClient()

	if err != nil {
		return nil, err
	}

	logger, err := logging.NewDevProdLogger()

	if err != nil {
		return nil, err
	}

	bCache, err := bigcache.New(context.Background(), bigcache.DefaultConfig(5*time.Minute))

	return &LiveOctopusClient{
		client:    client,
		logger:    logger,
		versioner: versioner,
		bigCache:  bCache,
	}, nil
}

func (o *LiveOctopusClient) IsDeployed(projectId string, releaseVersion string, environmentName string) (bool, error) {
	releases, err := o.client.Releases.Get(octopusdeploy.ReleasesQuery{
		IDs:                nil,
		IgnoreChannelRules: false,
		Skip:               0,
		Take:               10000,
	})

	if err != nil {
		return false, err
	}

	projectReleases := lo.Filter(releases.Items, func(item *octopusdeploy.Release, index int) bool {
		return item.ProjectID == projectId && item.Version == releaseVersion
	})

	if len(projectReleases) == 0 {
		return false, nil
	}

	deployments, err := o.client.Deployments.GetDeployments(projectReleases[0], &octopusdeploy.DeploymentQuery{
		Skip: 0,
		Take: 10000,
	})

	if err != nil {
		return false, err
	}

	if len(deployments.Items) == 0 {
		return false, nil
	}

	environmentId, err := o.getEnvironmentId(environmentName)

	if err != nil {
		return false, err
	}

	environmentDeployments := lo.Filter(deployments.Items, func(item *octopusdeploy.Deployment, index int) bool {
		return item.EnvironmentID == environmentId
	})

	return len(environmentDeployments) != 0, nil
}

func (o *LiveOctopusClient) GetReleaseVersions(projectId string) ([]string, error) {
	releases, err := o.client.Releases.Get(octopusdeploy.ReleasesQuery{
		IDs:                nil,
		IgnoreChannelRules: false,
		Skip:               0,
		Take:               1000,
	})

	if err != nil {
		return nil, err
	}

	projectReleases := lo.FilterMap(releases.Items, func(item *octopusdeploy.Release, index int) (string, bool) {
		return item.Version, item.ProjectID == projectId
	})

	return projectReleases, nil
}

func (o *LiveOctopusClient) CreateAndDeployRelease(updateMessage models.ApplicationUpdateMessage) error {
	projects, err := o.getProject(updateMessage.Application, updateMessage.Namespace)

	if err != nil {
		return err
	}

	if len(projects) == 0 {
		o.logger.GetLogger().Info("No projects found configured for " + updateMessage.Application + " in namespace " + updateMessage.Namespace)
		o.logger.GetLogger().Info("To create releases for this application, add the Metadata.ArgoCD.Application[" +
			updateMessage.Namespace + "/" + updateMessage.Application + "].Environment variable with a value matching the application's environment name, like \"Development\"")
	}

	for _, project := range projects {

		defaultChannel, err := o.getDefaultChannel(project.Project)

		if err != nil {
			return err
		}

		version, err := o.versioner.GenerateReleaseVersion(o, project, updateMessage)

		if err != nil {
			return err
		}

		release, err := o.getRelease(project, version, defaultChannel.ID)

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

		o.logger.GetLogger().Info("Created release " + release.ID + " with version " + version + " and deployment " + deployment.ID +
			" in environment " + project.Environment + " for project " + project.Project.Name)

	}

	return nil
}

func getClient() (*octopusdeploy.Client, error) {
	if os.Getenv("OCTOPUS_SERVER") == "" {
		return nil, errors.New("octoargosync-init-octoclienterror - OCTOPUS_SERVER must be defined")
	}

	if os.Getenv("OCTOPUS_API_KEY") == "" {
		return nil, errors.New("octoargosync-init-octoclienterror - OCTOPUS_API_KEY must be defined")
	}

	octopusUrl, err := url.Parse(os.Getenv("OCTOPUS_SERVER"))

	if err != nil {
		return nil, fmt.Errorf("octoargosync-init-octoclienterror - failed to parse OCTOPUS_SERVER as a url: %w", err)
	}

	client, err := octopusdeploy.NewClient(nil, octopusUrl, os.Getenv("OCTOPUS_API_KEY"), os.Getenv("OCTOPUS_SPACE_ID"))

	if err != nil {
		return nil, fmt.Errorf("octoargosync-init-octoclienterror - failed to create the Octopus API client. Check that the OCTOPUS_SERVER, OCTOPUS_API_KEY, and OCTOPUS_SPACE_ID environment variables are valid: %w", err)
	}

	return client, nil
}

// getProject scans Octopus for the project that has been linked to the Argo CD Application and namespace
func (o *LiveOctopusClient) getRelease(project models.ArgoCDProject, version string, channelId string) (*octopusdeploy.Release, error) {

	releases, err := o.client.Releases.Get(octopusdeploy.ReleasesQuery{
		IDs:                nil,
		IgnoreChannelRules: false,
		Skip:               0,
		Take:               10000,
	})

	if err != nil {
		return nil, err
	}

	existingReleases := lo.Filter(releases.Items, func(item *octopusdeploy.Release, index int) bool {
		return item.ProjectID == project.Project.ID && item.Version == version
	})

	if len(existingReleases) == 0 {
		release := octopusdeploy.NewRelease(channelId, project.Project.ID, version)
		return o.client.Releases.Add(release)
	} else {
		return existingReleases[0], nil
	}
}

// getProject scans Octopus for the project that has been linked to the Argo CD Application and namespace
func (o *LiveOctopusClient) getProject(application string, namespace string) ([]models.ArgoCDProject, error) {

	projects, err := o.getAllProject()

	if err != nil {
		return nil, err
	}

	matchingProjects := lo.FilterMap(projects.Items, func(project *octopusdeploy.Project, index int) (models.ArgoCDProject, bool) {
		variables, err := o.getProjectVariables(project.ID)

		if err != nil {
			return models.ArgoCDProject{}, false
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
			return models.ArgoCDProject{
				Project:             project,
				Environment:         appNameEnvironments[0],
				ReleaseVersionImage: releaseVersionImage,
			}, true
		}

		return models.ArgoCDProject{}, false
	})

	return matchingProjects, nil
}

func (o *LiveOctopusClient) getProjectVariables(projectId string) (*octopusdeploy.VariableSet, error) {
	// Load variables, and cache the results
	variables := &octopusdeploy.VariableSet{}
	variablesData, err := o.bigCache.Get(projectId + "-Variables")
	if err == nil {
		err = json.Unmarshal(variablesData, variables)

		if err != nil {
			return nil, err
		}
	} else {
		freshVariables, err := o.client.Variables.GetAll(projectId)
		variables = &freshVariables

		if err != nil {
			return nil, err
		}

		variablesData, err = json.Marshal(freshVariables)

		if err != nil {
			return nil, err
		}

		o.bigCache.Set(projectId+"-Variables", variablesData)
	}

	return variables, nil
}

func (o *LiveOctopusClient) getAllProject() (*octopusdeploy.Projects, error) {
	// Load projects, and cache the results
	projects := &octopusdeploy.Projects{}
	projectsData, err := o.bigCache.Get("AllProjects")
	if err == nil {
		err = json.Unmarshal(projectsData, projects)

		if err != nil {
			return nil, err
		}
	} else {
		projects, err = o.client.Projects.Get(octopusdeploy.ProjectsQuery{Take: MaxInt})

		if err != nil {
			return nil, err
		}

		projectsData, err = json.Marshal(projects)

		if err != nil {
			return nil, err
		}

		o.bigCache.Set("AllProjects", projectsData)
	}

	return projects, nil
}

func (o *LiveOctopusClient) getDefaultChannel(project *octopusdeploy.Project) (*octopusdeploy.Channel, error) {

	// Load variables, and cache the results
	channel := &octopusdeploy.Channel{}
	channelData, err := o.bigCache.Get(project.ID + "-DefaultChannel")
	if err == nil {
		err = json.Unmarshal(channelData, channel)

		if err != nil {
			return nil, err
		}

		return channel, nil
	} else {
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

		if len(defaultChannel) != 1 {
			return nil, errors.New("could not find the default channel")
		}

		channelData, err = json.Marshal(defaultChannel[0])

		if err != nil {
			return nil, err
		}

		o.bigCache.Set(project.ID+"-DefaultChannel", channelData)

		return defaultChannel[0], nil
	}
}

func (o *LiveOctopusClient) getEnvironmentId(environmentName string) (string, error) {
	if strings.HasPrefix("Environments-", environmentName) {
		return environmentName, nil
	}

	// Load environments, and cache the results
	environment := &octopusdeploy.Environment{}
	environmentData, err := o.bigCache.Get("Environments-" + environmentName)
	if err == nil {
		err = json.Unmarshal(environmentData, environment)

		if err != nil {
			return "", err
		}

		return environment.ID, nil
	} else {
		environmentsQuery := octopusdeploy.EnvironmentsQuery{
			Name: environmentName,
		}

		environments, err := o.client.Environments.Get(environmentsQuery)

		if err != nil {
			return "", nil
		}

		filteredEnvironments := lo.Filter(environments.Items, func(e *octopusdeploy.Environment, index int) bool {
			return e.Name == environmentName
		})

		if len(filteredEnvironments) != 1 {
			return "", errors.New("failed to find an environment called " + environmentName)
		}

		environmentData, err = json.Marshal(filteredEnvironments[0])

		if err != nil {
			return "", err
		}

		o.bigCache.Set("Environments-"+environmentName, environmentData)

		return filteredEnvironments[0].ID, nil
	}
}
