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
	"github.com/hashicorp/go-multierror"
	"github.com/samber/lo"
	"golang.org/x/exp/slices"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const MaxInt = 2147483647

var ApplicationEnvironmentVariable = regexp.MustCompile("^Metadata.ArgoCD\\.Application\\[([^\\[\\]]*?)]\\.Environment$")
var ApplicationChannelVariable = regexp.MustCompile("^Metadata.ArgoCD\\.Application\\[([^\\[\\]]*?)]\\.Channel$")
var ApplicationImageReleaseVersionVariable = regexp.MustCompile("^Metadata.ArgoCD\\.Application\\[([^\\[\\]]*?)]\\.ImageForReleaseVersion$")
var ApplicationImagePackageVersionVariable = regexp.MustCompile("^Metadata.ArgoCD\\.Application\\[([^\\[\\]]*?)]\\.ImageForPackageVersion\\[([^\\[\\]]*?)]$")

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

	var result error

	for _, project := range projects {

		channel, err := o.getArgoCdChannel(project)

		if err != nil {
			result = multierror.Append(result, err)
			continue
		}

		environmentId, err := o.getEnvironmentId(project.Environment)

		if err != nil {
			return errors.New("failed to find an environment called " + project.Environment +
				" - ensure the variable Metadata.ArgoCD.Application[" +
				updateMessage.Namespace + "/" + updateMessage.Application + "].Environment is set to a valid environment name")
		}

		lifecycle, err := o.getLifecycle(channel.LifecycleID)

		if err != nil {
			result = multierror.Append(result, err)
			continue
		}

		err = o.validateLifecycle(lifecycle, environmentId)

		if err != nil {
			result = multierror.Append(result, err)
			continue
		}

		version, err := o.versioner.GenerateReleaseVersion(o, project, updateMessage)

		if err != nil {
			result = multierror.Append(result, err)
			continue
		}

		release, newRelease, err := o.getRelease(project, version, channel.ID, updateMessage)

		if err != nil {
			result = multierror.Append(result, err)
			continue
		}

		if newRelease && slices.Index(lifecycle.Phases[0].AutomaticDeploymentTargets, environmentId) != -1 {
			o.logger.GetLogger().Info("Created release " + release.ID + " with version " + version + " for project " + project.Project.Name)
			o.logger.GetLogger().Info("The environment " + project.Environment + " is an automatic deployment target in the first phase, so Octopus will automatically deploy the release")
			continue
		}

		deployment := octopusdeploy.NewDeployment(environmentId, release.ID)
		deployment, err = o.client.Deployments.Add(deployment)

		if err != nil {
			result = multierror.Append(result, err)
			continue
		}

		o.logger.GetLogger().Info("Created release " + release.ID + " with version " + version + " and deployment " + deployment.ID +
			" in environment " + project.Environment + " for project " + project.Project.Name)

	}

	return err
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

func (o *LiveOctopusClient) getArgoCdChannel(project models.ArgoCDProject) (*octopusdeploy.Channel, error) {
	if project.Channel != "" {
		return o.getChannel(project.Project, project.Channel)
	}

	return o.getDefaultChannel(project.Project)
}

// validateLifecycle checks for some common misconfigurations and either throws an error or prints a warning
func (o *LiveOctopusClient) validateLifecycle(lifecycle *octopusdeploy.Lifecycle, environmentId string) error {
	if lifecycle == nil {
		return errors.New("lifecycle must not be nil")
	}

	if len(lifecycle.Phases) == 0 {
		return errors.New("the lifecycle " + lifecycle.Name + " has no phases, so deployment will fail")
	}

	allEnvironments := lo.FlatMap(lifecycle.Phases, func(item octopusdeploy.Phase, index int) []string {
		environments := []string{}
		environments = append(environments, item.AutomaticDeploymentTargets...)
		environments = append(environments, item.OptionalDeploymentTargets...)
		return environments
	})

	if slices.Index(allEnvironments, environmentId) == -1 {
		return errors.New("the lifecycle " + lifecycle.Name + " does not include the environment " + environmentId)
	}

	if slices.Index(lifecycle.Phases[0].AutomaticDeploymentTargets, environmentId) == -1 &&
		slices.Index(lifecycle.Phases[0].OptionalDeploymentTargets, environmentId) == -1 {
		o.logger.GetLogger().Warn("It is recommended that the lifecycle associated with the project includes all ArgoCD environments in the first phase +" +
			"because ArgoCD does not enforce any environment progression rules and deployments can happen to any environment in any order.")
	}

	return nil
}

// getPackages extracts packages and the images that the package versions are selected from
func (o *LiveOctopusClient) getPackages(project models.ArgoCDProject, updateMessage models.ApplicationUpdateMessage) ([]*octopusdeploy.SelectedPackage, error) {
	selectedPackages := []*octopusdeploy.SelectedPackage{}

	for _, imagePackageVersion := range project.PackageVersions {

		imageVersion := lo.FilterMap(updateMessage.Images, func(item string, index int) (string, bool) {
			split := strings.Split(item, ":")

			if len(split) != 2 {
				return "", false
			}

			return split[1], split[0] == imagePackageVersion.Image
		})

		if len(imageVersion) == 0 {
			o.logger.GetLogger().Error("The ArgoCD deployment does not contain an image called " + imagePackageVersion.Image + " so the default package version will be used.")
			continue
		}

		split := strings.Split(imagePackageVersion.PackageReference, ":")

		if len(split) == 1 {
			selectedPackages = append(selectedPackages, &octopusdeploy.SelectedPackage{
				ActionName:           split[0],
				PackageReferenceName: "",
				StepName:             "",
				Version:              imageVersion[0],
			})
		} else if len(split) == 2 {
			selectedPackages = append(selectedPackages, &octopusdeploy.SelectedPackage{
				ActionName:           split[0],
				PackageReferenceName: split[1],
				StepName:             "",
				Version:              imageVersion[0],
			})
		} else {
			o.logger.GetLogger().Error("The step package reference " + imagePackageVersion.PackageReference + " was in an unexpected format. It must be a string separated by 0 or 1 colons e.g. stepname, stepname:packagename")
		}
	}

	return selectedPackages, nil
}

// getRelease finds the release for a given version in a project, or it creates a new release.
func (o *LiveOctopusClient) getRelease(project models.ArgoCDProject, version string, channelId string, updateMessage models.ApplicationUpdateMessage) (*octopusdeploy.Release, bool, error) {

	releases, err := o.client.Releases.Get(octopusdeploy.ReleasesQuery{
		IDs:                nil,
		IgnoreChannelRules: false,
		Skip:               0,
		Take:               10000,
	})

	if err != nil {
		return nil, false, err
	}

	existingReleases := lo.Filter(releases.Items, func(item *octopusdeploy.Release, index int) bool {
		return item.ProjectID == project.Project.ID && item.Version == version
	})

	packages, err := o.getPackages(project, updateMessage)

	if err != nil {
		return nil, false, err
	}

	if len(existingReleases) == 0 {
		release := &octopusdeploy.Release{
			ChannelID:        channelId,
			ProjectID:        project.Project.ID,
			Version:          version,
			SelectedPackages: packages,
		}

		release, err := o.client.Releases.Add(release)
		return release, true, err
	} else {
		return existingReleases[0], false, nil
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

		appNameChannel := lo.FilterMap(variables.Variables, func(variable *octopusdeploy.Variable, index int) (string, bool) {
			match := ApplicationChannelVariable.FindStringSubmatch(variable.Name)

			if len(match) != 2 || match[1] != namespace+"/"+application {
				return "", false
			}

			return variable.Value, true
		})

		channel := ""
		if len(appNameChannel) != 0 {
			channel = appNameChannel[0]
		}

		releaseVersionImages := lo.FilterMap(variables.Variables, func(variable *octopusdeploy.Variable, index int) (string, bool) {
			match := ApplicationImageReleaseVersionVariable.FindStringSubmatch(variable.Name)

			if len(match) != 2 || match[1] != namespace+"/"+application {
				return "", false
			}

			return variable.Value, true
		})

		packageVersionImages := lo.FilterMap(variables.Variables, func(variable *octopusdeploy.Variable, index int) (models.ImagePackageVersion, bool) {
			match := ApplicationImagePackageVersionVariable.FindStringSubmatch(variable.Name)

			if len(match) != 3 || match[1] != namespace+"/"+application {
				return models.ImagePackageVersion{}, false
			}

			return models.ImagePackageVersion{
				Image:            match[2],
				PackageReference: variable.Value,
			}, true
		})

		releaseVersionImage := ""
		if len(releaseVersionImages) != 0 {
			releaseVersionImage = releaseVersionImages[0]
		}

		if len(appNameEnvironments) != 0 {
			return models.ArgoCDProject{
				Project:             project,
				Environment:         appNameEnvironments[0],
				Channel:             channel,
				ReleaseVersionImage: releaseVersionImage,
				PackageVersions:     packageVersionImages,
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

		err = o.bigCache.Set(projectId+"-Variables", variablesData)

		if err != nil {
			return nil, err
		}
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

		err = o.bigCache.Set("AllProjects", projectsData)

		if err != nil {
			return nil, err
		}
	}

	return projects, nil
}

func (o *LiveOctopusClient) getLifecycle(lifecycleId string) (*octopusdeploy.Lifecycle, error) {
	lifecycle := &octopusdeploy.Lifecycle{}
	lifecycleData, err := o.bigCache.Get(lifecycleId)

	if err == nil {
		err = json.Unmarshal(lifecycleData, lifecycle)

		if err != nil {
			return nil, err
		}

		return lifecycle, nil
	} else {
		lifecycleQuery := octopusdeploy.LifecyclesQuery{
			IDs:         []string{lifecycleId},
			PartialName: "",
			Skip:        0,
			Take:        1,
		}

		lifecycles, err := o.client.Lifecycles.Get(lifecycleQuery)

		if err != nil {
			return nil, nil
		}

		if len(lifecycles.Items) != 1 {
			return nil, errors.New("failed to find lifecycle with ID " + lifecycleId)
		}

		lifecyclesData, err := json.Marshal(lifecycles.Items[0])

		if err != nil {
			return nil, err
		}

		err = o.bigCache.Set(lifecycleId, lifecyclesData)

		if err != nil {
			return nil, err
		}

		return lifecycles.Items[0], nil
	}
}

func (o *LiveOctopusClient) getChannel(project *octopusdeploy.Project, channel string) (*octopusdeploy.Channel, error) {
	// Load variables, and cache the results
	channels := &octopusdeploy.Channels{}
	channelData, err := o.bigCache.Get("AllChannels")

	if err == nil {
		err = json.Unmarshal(channelData, channels)

		if err != nil {
			return nil, err
		}
	} else {
		channelQuery := octopusdeploy.ChannelsQuery{
			Take: MaxInt,
			Skip: 0,
		}

		channels, err = o.client.Channels.Get(channelQuery)

		if err != nil {
			return nil, err
		}

		channelsData, err := json.Marshal(channels)

		if err != nil {
			return nil, err
		}

		err = o.bigCache.Set("AllChannels", channelsData)
	}

	channelResource := lo.Filter(channels.Items, func(item *octopusdeploy.Channel, index int) bool {
		return item.Name == channel && item.ProjectID == project.ID
	})

	if len(channelResource) != 1 {
		return nil, errors.New("could not find the channel called " + channel + " for the project " + project.Name)
	}

	return channelResource[0], nil
}

func (o *LiveOctopusClient) getDefaultChannel(project *octopusdeploy.Project) (*octopusdeploy.Channel, error) {
	if project == nil {
		return nil, errors.New("project must not be nil")
	}

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

		err = o.bigCache.Set(project.ID+"-DefaultChannel", channelData)

		if err != nil {
			return nil, err
		}

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

		err = o.bigCache.Set("Environments-"+environmentName, environmentData)

		if err != nil {
			return "", err
		}

		return filteredEnvironments[0].ID, nil
	}
}
