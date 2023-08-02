package octopus

import (
	"errors"
	"github.com/OctopusDeploy/go-octopusdeploy/octopusdeploy"
	"github.com/samber/lo"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"
	"net/url"
	"os"
	"regexp"
	"strings"
)

const MaxInt = 4294967295

var ApplicationNameVariable = regexp.MustCompile("^ArgoCD.Application\\[([^[]]*?)].Environment$")
var ApplicationNamespaceVariable = regexp.MustCompile("^ArgoCD.Application\\[([^[]]*?)].Namespace$")

type ArgoCDProject struct {
	Project      *octopusdeploy.Project
	Environments []string
}

type LiveOctopusClient struct {
	client *octopusdeploy.Client
	logger *zap.Logger
}

func NewLiveOctopusClient() (*LiveOctopusClient, error) {
	client, err := getClient()

	if err != nil {
		return nil, err
	}

	logger, err := zap.NewProduction()

	if err != nil {
		return nil, err
	}

	return &LiveOctopusClient{
		client: client,
		logger: logger,
	}, nil
}

func (o *LiveOctopusClient) CreateAndDeployRelease(application string, namespace string) error {
	releaseVersion := "SHA and datetime"

	projects, err := o.getProject(application, namespace)

	if err != nil {
		return err
	}

	for _, project := range projects {
		defaultChannel, err := o.getDefaultChannel(project.Project)

		if err != nil {
			return err
		}

		release := octopusdeploy.NewRelease(defaultChannel.ID, project.Project.ID, releaseVersion)
		release, err = o.client.Releases.Add(release)

		for _, environment := range project.Environments {

			environmentId, err := o.getEnvironmentId(environment)

			if err != nil {
				return err
			}

			deployment := octopusdeploy.NewDeployment(environmentId, release.ID)
			deployment, err = o.client.Deployments.Add(deployment)
		}
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
func (o *LiveOctopusClient) getProject(application string, namespace string) ([]*ArgoCDProject, error) {
	projects, err := o.client.Projects.Get(octopusdeploy.ProjectsQuery{Take: MaxInt})

	if err != nil {
		return nil, err
	}

	matchingProjects := lo.FilterMap(projects.Items, func(project *octopusdeploy.Project, index int) (*ArgoCDProject, bool) {
		variables, err := o.client.Variables.GetAll(project.ID)

		if err != nil {
			return nil, false
		}

		appNameEnvironments := lo.FlatMap(variables.Variables, func(variable *octopusdeploy.Variable, index int) []string {
			match := ApplicationNameVariable.FindStringSubmatch(variable.Name)

			if len(match) != 2 || match[1] != namespace {
				return []string{}
			}

			return match[1:1]
		})

		appNamespaceEnvironments := lo.FlatMap(variables.Variables, func(variable *octopusdeploy.Variable, index int) []string {
			match := ApplicationNamespaceVariable.FindStringSubmatch(variable.Name)

			if len(match) != 2 || match[1] != application {
				return []string{}
			}

			return match[1:1]
		})

		commonEnvironments := lo.Filter(appNameEnvironments, func(item string, index int) bool {
			return slices.Index(appNamespaceEnvironments, item) != -1
		})

		return &ArgoCDProject{
			Project:      project,
			Environments: commonEnvironments,
		}, true
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
