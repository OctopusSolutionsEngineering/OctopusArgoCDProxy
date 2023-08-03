package octopus

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/OctopusDeploy/go-octopusdeploy/octopusdeploy"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/versioning"
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
	versioner versioning.ReleaseVersioner
	bigCache  *bigcache.BigCache
}

func NewLiveOctopusClient(versioner versioning.ReleaseVersioner) (*LiveOctopusClient, error) {
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

func (o *LiveOctopusClient) CreateAndDeployRelease(updateMessage models.ApplicationUpdateMessage) error {
	projects, err := o.getProject(updateMessage.Application, updateMessage.Namespace)

	if err != nil {
		return err
	}

	for _, project := range projects {

		defaultChannel, err := o.getDefaultChannel(project.Project)

		if err != nil {
			return err
		}

		version := o.versioner.GenerateReleaseVersion(project, updateMessage)

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

		o.logger.GetLogger().Info("Created release " + release.ID + " with version " + version + " and deployment " + deployment.ID +
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
