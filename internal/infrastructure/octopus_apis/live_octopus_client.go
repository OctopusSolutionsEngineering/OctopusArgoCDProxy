package octopus_apis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/OctopusDeploy/go-octopusdeploy/octopusdeploy"
	"github.com/OctopusDeploy/go-octopusdeploy/v2/pkg/channels"
	octopusApiClient "github.com/OctopusDeploy/go-octopusdeploy/v2/pkg/client"
	"github.com/OctopusDeploy/go-octopusdeploy/v2/pkg/deployments"
	"github.com/OctopusDeploy/go-octopusdeploy/v2/pkg/feeds"
	"github.com/OctopusDeploy/go-octopusdeploy/v2/pkg/releases"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/apploggers"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/retry_config"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/types"
	"github.com/allegro/bigcache/v3"
	"github.com/avast/retry-go"
	"github.com/samber/lo"
	"golang.org/x/exp/slices"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const MaxInt = 2147483647

var ApplicationEnvironmentVariable = regexp.MustCompile("^Metadata.ArgoCD\\.Application\\[([^\\[\\]]*?)]\\.Environment$")
var ApplicationChannelVariable = regexp.MustCompile("^Metadata.ArgoCD\\.Application\\[([^\\[\\]]*?)]\\.Channel$")
var ApplicationImageReleaseVersionVariable = regexp.MustCompile("^Metadata.ArgoCD\\.Application\\[([^\\[\\]]*?)]\\.ImageForReleaseVersion$")
var ApplicationImagePackageVersionVariable = regexp.MustCompile("^Metadata.ArgoCD\\.Application\\[([^\\[\\]]*?)]\\.ImageForPackageVersion\\[([^\\[\\]]*?)]$")

// LiveOctopusClient interacts with a live Octopus API endpoint, and implements caching to reduce network calls.
type LiveOctopusClient struct {
	client       *octopusdeploy.Client
	logger       apploggers.AppLogger
	bigCache     *bigcache.BigCache
	applications sync.Map
}

func NewLiveOctopusClient() (*LiveOctopusClient, error) {
	client, err := getClient()

	if err != nil {
		return nil, err
	}

	logger, err := apploggers.NewDevProdLogger()

	if err != nil {
		return nil, err
	}

	bCache, err := bigcache.New(context.Background(), bigcache.DefaultConfig(5*time.Minute))

	return &LiveOctopusClient{
		client:       client,
		logger:       logger,
		bigCache:     bCache,
		applications: sync.Map{},
	}, nil
}

func (o *LiveOctopusClient) IsDeployed(project *octopusdeploy.Project, releaseVersion types.OctopusReleaseVersion, environment *octopusdeploy.Environment) (bool, error) {
	var octopusReleases []*octopusdeploy.Release
	err := retry.Do(
		func() error {
			var err error
			octopusReleases, err = o.client.Projects.GetReleases(project)
			return err
		}, retry_config.RetryOptions...)

	if err != nil {
		return false, err
	}

	projectReleases := lo.Filter(octopusReleases, func(item *octopusdeploy.Release, index int) bool {
		return item.Version == fmt.Sprint(releaseVersion)
	})

	if len(projectReleases) == 0 {
		return false, nil
	}

	var octopusDeployments *octopusdeploy.Deployments
	err = retry.Do(
		func() error {
			var err error
			octopusDeployments, err = o.client.Deployments.GetDeployments(projectReleases[0], &octopusdeploy.DeploymentQuery{
				Skip: 0,
				Take: 10000,
			})
			return err
		}, retry_config.RetryOptions...)

	if err != nil {
		return false, err
	}

	if len(octopusDeployments.Items) == 0 {
		return false, nil
	}

	environmentDeployments := lo.Filter(octopusDeployments.Items, func(item *octopusdeploy.Deployment, index int) bool {
		return item.EnvironmentID == environment.ID
	})

	return len(environmentDeployments) != 0, nil
}

func (o *LiveOctopusClient) GetLatestDeploymentRelease(project *octopusdeploy.Project, environment *octopusdeploy.Environment) (*octopusdeploy.Release, error) {
	var octopusReleases []*octopusdeploy.Release
	err := retry.Do(
		func() error {
			var err error
			octopusReleases, err = o.client.Projects.GetReleases(project)
			return err
		}, retry_config.RetryOptions...)

	if err != nil {
		return nil, err
	}

	if len(octopusReleases) == 0 {
		return nil, nil
	}

	slices.SortFunc(octopusReleases, func(a, b *octopusdeploy.Release) bool {
		return a.Assembled.After(b.Assembled)
	})

	for _, release := range octopusReleases {
		progression, err := o.client.Deployments.GetProgression(release)

		if err != nil {
			return nil, err
		}

		_, exists := lo.Find(progression.Environments, func(item *octopusdeploy.ReferenceDataItem) bool {
			return item.ID == environment.ID
		})

		if exists {
			return release, nil
		}
	}

	return nil, nil
}

func (o *LiveOctopusClient) GetLatestRelease(project *octopusdeploy.Project) (*octopusdeploy.Release, error) {
	var octopusReleases []*octopusdeploy.Release
	err := retry.Do(
		func() error {
			var err error
			octopusReleases, err = o.client.Projects.GetReleases(project)
			return err
		}, retry_config.RetryOptions...)

	if err != nil {
		return nil, err
	}

	if len(octopusReleases) == 0 {
		return nil, nil
	}

	slices.SortFunc(octopusReleases, func(a, b *octopusdeploy.Release) bool {
		return a.Assembled.After(b.Assembled)
	})

	return octopusReleases[0], nil
}

func (o *LiveOctopusClient) GetReleaseVersions(project *octopusdeploy.Project) ([]types.OctopusReleaseVersion, error) {
	var octopusReleases []*octopusdeploy.Release
	err := retry.Do(
		func() error {
			var err error
			octopusReleases, err = o.client.Projects.GetReleases(project)
			return err
		}, retry_config.RetryOptions...)

	if err != nil {
		return nil, err
	}

	projectReleases := lo.Map(octopusReleases, func(item *octopusdeploy.Release, index int) types.OctopusReleaseVersion {
		return types.OctopusReleaseVersion(item.Version)
	})

	return projectReleases, nil
}

func (o *LiveOctopusClient) GetProjects(updateMessage models.ApplicationUpdateMessage) ([]models.ArgoCDProjectExpanded, error) {
	allProjects, err := o.getAllProjectAndVariables(updateMessage)

	if err != nil {
		return nil, err
	}

	projects, err := o.getProjectsMatchingArgoCDApplication(allProjects, updateMessage.Application, updateMessage.Namespace)

	if err != nil {
		return nil, err
	}

	return o.expandProjectReferences(projects)
}

func (o *LiveOctopusClient) CreateAndDeployRelease(project models.ArgoCDProjectExpanded, updateMessage models.ApplicationUpdateMessage, version types.OctopusReleaseVersion) error {

	err := o.validateLifecycle(project.Lifecycle, project.Environment)

	if err != nil {
		return err
	}

	release, newRelease, err := o.getRelease(project, version, project.Channel, updateMessage)

	if err != nil {
		return err
	}

	if newRelease && slices.Index(project.Lifecycle.Phases[0].AutomaticDeploymentTargets, project.Environment.ID) != -1 {
		o.logger.GetLogger().Info("Created release " + release.ID + " with version " + fmt.Sprint(version) + " for project " + project.Project.Name)
		o.logger.GetLogger().Info("The environment " + project.Environment.Name + " is an automatic deployment target in the first phase, so Octopus will automatically deploy the release")
		return nil
	}

	deployment := octopusdeploy.NewDeployment(project.Environment.ID, release.ID)
	deployment, err = o.client.Deployments.Add(deployment)

	if err != nil {
		return err
	}

	o.logger.GetLogger().Info("Created release " + release.ID + " with version " + fmt.Sprint(version) + " and deployment " + deployment.ID +
		" in environment " + project.Environment.Name + " for project " + project.Project.Name)

	return nil
}

// getClient2 returns a client for the version 2 octopus_apis go library
func getClient2() (*octopusApiClient.Client, error) {
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

	client, err := octopusApiClient.NewClient(nil, octopusUrl, os.Getenv("OCTOPUS_API_KEY"), os.Getenv("OCTOPUS_SPACE_ID"))

	if err != nil {
		return nil, fmt.Errorf("octoargosync-init-octoclienterror - failed to create the Octopus API client. Check that the OCTOPUS_SERVER, OCTOPUS_API_KEY, and OCTOPUS_SPACE_ID environment variables are valid: %w", err)
	}

	return client, nil
}

// getClient returns a client for the version 1 octopus_apis go library
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

// getArgoCdChannel returns the default channel if no channel was indicated on the project, otherwise the specific channel is returned.
func (o *LiveOctopusClient) getArgoCdChannel(project models.ArgoCDProject) (*octopusdeploy.Channel, error) {
	if project.ChannelName != "" {
		return o.getChannel(project.Project, project.ChannelName)
	}

	return o.getDefaultChannel(project.Project)
}

// validateLifecycle checks for some common misconfigurations and either throws an error or prints a warning
func (o *LiveOctopusClient) validateLifecycle(lifecycle *octopusdeploy.Lifecycle, environment *octopusdeploy.Environment) error {
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

	if slices.Index(allEnvironments, environment.ID) == -1 {
		return errors.New("the lifecycle " + lifecycle.Name + " does not include the environment " + environment.ID)
	}

	if slices.Index(lifecycle.Phases[0].AutomaticDeploymentTargets, environment.ID) == -1 &&
		slices.Index(lifecycle.Phases[0].OptionalDeploymentTargets, environment.ID) == -1 {
		o.logger.GetLogger().Warn("It is recommended that the lifecycle associated with the project includes all ArgoCD environments in the first phase +" +
			"because ArgoCD does not enforce any environment progression rules and deployments can happen to any environment in any order.")
	}

	return nil
}

// getDefaultPackages gets the default package versions for the project
func (o *LiveOctopusClient) getDefaultPackages(project models.ArgoCDProjectExpanded, channelId string) ([]*octopusdeploy.SelectedPackage, error) {
	octopus, err := getClient2()

	if err != nil {
		return nil, err
	}

	deploymentProcess, err := octopus.DeploymentProcesses.GetByID(project.Project.DeploymentProcessID)

	if err != nil {
		return nil, err
	}

	channel, err := octopus.Channels.GetByID(channelId)

	if err != nil {
		return nil, err
	}

	deploymentProcessTemplate, err := octopus.DeploymentProcesses.GetTemplate(deploymentProcess, channelId, "")

	if err != nil {
		return nil, err
	}

	return o.buildPackageVersionBaseline(octopus, deploymentProcessTemplate, channel)
}

// getPackages extracts packages and the images that the package versions are selected from
func (o *LiveOctopusClient) getPackages(project models.ArgoCDProjectExpanded, updateMessage models.ApplicationUpdateMessage) ([]*octopusdeploy.SelectedPackage, error) {
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
			o.logger.GetLogger().Error("octoargosync-init-argoimagenotfound: The ArgoCD deployment does not contain an image called " + imagePackageVersion.Image + " so the default package version will be used.")
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
			o.logger.GetLogger().Error("octoargosync-init-octopackagereferenceerror: The step package reference " + imagePackageVersion.PackageReference + " was in an unexpected format. It must be a string separated by 0 or 1 colons e.g. stepname, stepname:packagename")
		}
	}

	return selectedPackages, nil
}

// getRelease finds the release for a given version in a project, or it creates a new release.
func (o *LiveOctopusClient) getRelease(project models.ArgoCDProjectExpanded, version types.OctopusReleaseVersion, channel *octopusdeploy.Channel, updateMessage models.ApplicationUpdateMessage) (*octopusdeploy.Release, bool, error) {
	var octopusReleases *octopusdeploy.Releases
	err := retry.Do(
		func() error {
			var err error
			octopusReleases, err = o.client.Releases.Get(octopusdeploy.ReleasesQuery{
				IDs:                nil,
				IgnoreChannelRules: false,
				Skip:               0,
				Take:               10000,
			})
			return err
		}, retry_config.RetryOptions...)

	if err != nil {
		return nil, false, err
	}

	existingReleases := lo.Filter(octopusReleases.Items, func(item *octopusdeploy.Release, index int) bool {
		return item.ProjectID == project.Project.ID && item.Version == fmt.Sprint(version)
	})

	// Get the package versions that are mapped by the project metadata
	packages, err := o.getPackages(project, updateMessage)

	if err != nil {
		return nil, false, err
	}

	// Get the latest package versions
	defaultPackages, err := o.getDefaultPackages(project, channel.ID)

	if err != nil {
		return nil, false, err
	}

	// override any default packages with those versions that are specifically configured
	finalPackages := o.overridePackageSelections(defaultPackages, packages)

	if len(existingReleases) == 0 {
		release := &octopusdeploy.Release{
			ChannelID:        channel.ID,
			ProjectID:        project.Project.ID,
			Version:          fmt.Sprint(version),
			SelectedPackages: finalPackages,
		}

		release, err := o.client.Releases.Add(release)
		return release, true, err
	} else {
		return existingReleases[0], false, nil
	}
}

// overridePackageSelections returns package selections with overrides applied to them
func (o *LiveOctopusClient) overridePackageSelections(defaultPackages []*octopusdeploy.SelectedPackage, packages []*octopusdeploy.SelectedPackage) []*octopusdeploy.SelectedPackage {
	if defaultPackages == nil {
		defaultPackages = []*octopusdeploy.SelectedPackage{}
	}

	if packages == nil {
		packages = []*octopusdeploy.SelectedPackage{}
	}

	return lo.Map(defaultPackages, func(item *octopusdeploy.SelectedPackage, index int) *octopusdeploy.SelectedPackage {
		override, found := lo.Find(packages, func(overridePackage *octopusdeploy.SelectedPackage) bool {
			return overridePackage.ActionName == item.ActionName &&
				overridePackage.StepName == item.StepName &&
				overridePackage.PackageReferenceName == item.StepName
		})

		if found {
			return override
		}

		return item
	})
}

// expandProjectReferences maps a project to the octopus_apis resources noted in the metadata variables
func (o *LiveOctopusClient) expandProjectReferences(projects []models.ArgoCDProject) ([]models.ArgoCDProjectExpanded, error) {
	expandedProjects := []models.ArgoCDProjectExpanded{}
	for _, project := range projects {
		environment, err := o.getEnvironment(project.EnvironmentName)

		if err != nil {
			return nil, err
		}

		channel, err := o.getArgoCdChannel(project)

		if err != nil {
			return nil, err
		}

		lifecycle, err := o.getLifecycle(channel.LifecycleID)

		if err != nil {
			return nil, err
		}

		expandedProjects = append(expandedProjects, models.ArgoCDProjectExpanded{
			Project:             project.Project,
			Environment:         environment,
			Channel:             channel,
			Lifecycle:           lifecycle,
			ReleaseVersionImage: project.ReleaseVersionImage,
			PackageVersions:     project.PackageVersions,
		})
	}

	return expandedProjects, nil
}

// getProjectsMatchingArgoCDApplication scans Octopus for the project that has been linked to the Argo CD Application and namespace
func (o *LiveOctopusClient) getProjectsMatchingArgoCDApplication(allProjects []models.OctopusProjectAndVars, application string, namespace string) ([]models.ArgoCDProject, error) {

	matchingProjects := lo.FilterMap(allProjects, func(project models.OctopusProjectAndVars, index int) (models.ArgoCDProject, bool) {
		appNameEnvironments := lo.FilterMap(project.Variables.Variables, func(variable *octopusdeploy.Variable, index int) (string, bool) {
			match := ApplicationEnvironmentVariable.FindStringSubmatch(variable.Name)

			if len(match) != 2 || match[1] != namespace+"/"+application {
				return "", false
			}

			return variable.Value, len(strings.TrimSpace(variable.Value)) != 0
		})

		appNameChannel := lo.FilterMap(project.Variables.Variables, func(variable *octopusdeploy.Variable, index int) (string, bool) {
			match := ApplicationChannelVariable.FindStringSubmatch(variable.Name)

			if len(match) != 2 || match[1] != namespace+"/"+application {
				return "", false
			}

			return variable.Value, len(strings.TrimSpace(variable.Value)) != 0
		})

		channel := ""
		if len(appNameChannel) != 0 {
			channel = appNameChannel[0]
		}

		releaseVersionImages := lo.FilterMap(project.Variables.Variables, func(variable *octopusdeploy.Variable, index int) (string, bool) {
			match := ApplicationImageReleaseVersionVariable.FindStringSubmatch(variable.Name)

			if len(match) != 2 || match[1] != namespace+"/"+application {
				return "", false
			}

			return variable.Value, len(strings.TrimSpace(variable.Value)) != 0
		})

		packageVersionImages := lo.FilterMap(project.Variables.Variables, func(variable *octopusdeploy.Variable, index int) (models.ImagePackageVersion, bool) {
			match := ApplicationImagePackageVersionVariable.FindStringSubmatch(variable.Name)

			if len(match) != 3 || match[1] != namespace+"/"+application {
				return models.ImagePackageVersion{}, false
			}

			return models.ImagePackageVersion{
				Image:            variable.Value,
				PackageReference: match[2],
			}, len(strings.TrimSpace(variable.Value)) != 0
		})

		releaseVersionImage := ""
		if len(releaseVersionImages) != 0 {
			releaseVersionImage = releaseVersionImages[0]
		}

		if len(appNameEnvironments) != 0 {
			return models.ArgoCDProject{
				Project:             project.Project,
				EnvironmentName:     appNameEnvironments[0],
				ChannelName:         channel,
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
		err = retry.Do(
			func() error {
				freshVariables, err := o.client.Variables.GetAll(projectId)
				variables = &freshVariables
				return err
			}, retry_config.RetryOptions...)

		if err != nil {
			return nil, err
		}

		variablesData, err = json.Marshal(variables)

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

func (o *LiveOctopusClient) getAllProjectAndVariables(updateMessage models.ApplicationUpdateMessage) ([]models.OctopusProjectAndVars, error) {
	// See if we have encountered this application before
	_, exists := o.applications.Load(updateMessage.Namespace + "/" + updateMessage.Application)

	// Load projects, and cache the results
	octopusProjects := &octopusdeploy.Projects{}
	projectsData, err := o.bigCache.Get("AllProjects")

	// If this is a new application (i.e. we have never seen it before), skip the cache.
	// This lets us get a refreshed project list for new applications, which will likely
	// happen when a new ArgoCD project is created in Octopus and a new Application is created
	// in ArgoCD with the correct triggers configured.
	if err == nil && exists {
		err = json.Unmarshal(projectsData, octopusProjects)

		if err != nil {
			return nil, err
		}
	} else {
		// note the new application
		o.applications.Store(updateMessage.Namespace+"/"+updateMessage.Application, true)

		err = retry.Do(
			func() error {
				var err error
				octopusProjects, err = o.client.Projects.Get(octopusdeploy.ProjectsQuery{Take: MaxInt})
				return err
			}, retry_config.RetryOptions...)

		if err != nil {
			return nil, err
		}

		projectsData, err = json.Marshal(octopusProjects)

		if err != nil {
			return nil, err
		}

		err = o.bigCache.Set("AllProjects", projectsData)

		if err != nil {
			return nil, err
		}
	}

	projectAndVars := []models.OctopusProjectAndVars{}
	for _, project := range octopusProjects.Items {
		variables, err := o.getProjectVariables(project.ID)

		if err != nil {
			return nil, err
		}

		projectAndVars = append(projectAndVars, models.OctopusProjectAndVars{
			Project:   project,
			Variables: variables,
		})
	}

	return projectAndVars, nil
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

		var octopusLifecycles *octopusdeploy.Lifecycles
		err = retry.Do(
			func() error {
				var err error
				octopusLifecycles, err = o.client.Lifecycles.Get(lifecycleQuery)
				return err
			}, retry_config.RetryOptions...)

		if err != nil {
			return nil, nil
		}

		if len(octopusLifecycles.Items) != 1 {
			return nil, errors.New("failed to find lifecycle with ID " + lifecycleId)
		}

		lifecyclesData, err := json.Marshal(octopusLifecycles.Items[0])

		if err != nil {
			return nil, err
		}

		err = o.bigCache.Set(lifecycleId, lifecyclesData)

		if err != nil {
			return nil, err
		}

		return octopusLifecycles.Items[0], nil
	}
}

func (o *LiveOctopusClient) getChannel(project *octopusdeploy.Project, channel string) (*octopusdeploy.Channel, error) {
	// Load variables, and cache the results
	octopusChannels := &octopusdeploy.Channels{}
	channelData, err := o.bigCache.Get("AllChannels")

	if err == nil {
		err = json.Unmarshal(channelData, octopusChannels)

		if err != nil {
			return nil, err
		}
	} else {
		channelQuery := octopusdeploy.ChannelsQuery{
			Take: MaxInt,
			Skip: 0,
		}

		err = retry.Do(
			func() error {
				var err error
				octopusChannels, err = o.client.Channels.Get(channelQuery)
				return err
			}, retry_config.RetryOptions...)

		if err != nil {
			return nil, err
		}

		channelsData, err := json.Marshal(octopusChannels)

		if err != nil {
			return nil, err
		}

		err = o.bigCache.Set("AllChannels", channelsData)
	}

	channelResource := lo.Filter(octopusChannels.Items, func(item *octopusdeploy.Channel, index int) bool {
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

		var octopusChannels *octopusdeploy.Channels
		err = retry.Do(
			func() error {
				var err error
				octopusChannels, err = o.client.Channels.Get(channelQuery)
				return err
			}, retry_config.RetryOptions...)

		if err != nil {
			return nil, err
		}

		defaultChannel := lo.Filter(octopusChannels.Items, func(item *octopusdeploy.Channel, index int) bool {
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

func (o *LiveOctopusClient) getEnvironment(environmentName string) (*octopusdeploy.Environment, error) {
	// Load environments, and cache the results
	environment := &octopusdeploy.Environment{}
	environmentData, err := o.bigCache.Get("Environments-" + environmentName)
	if err == nil {
		err = json.Unmarshal(environmentData, environment)

		if err != nil {
			return nil, err
		}

		return environment, nil
	} else {
		environmentsQuery := octopusdeploy.EnvironmentsQuery{
			Name: environmentName,
		}

		var octopusEnvironments *octopusdeploy.Environments
		err = retry.Do(
			func() error {
				var err error
				octopusEnvironments, err = o.client.Environments.Get(environmentsQuery)
				return err
			}, retry_config.RetryOptions...)

		if err != nil {
			return nil, err
		}

		filteredEnvironments := lo.Filter(octopusEnvironments.Items, func(e *octopusdeploy.Environment, index int) bool {
			return e.Name == environmentName
		})

		if len(filteredEnvironments) != 1 {
			return nil, errors.New("failed to find an environment called " + environmentName)
		}

		environmentData, err = json.Marshal(filteredEnvironments[0])

		if err != nil {
			return nil, err
		}

		err = o.bigCache.Set("Environments-"+environmentName, environmentData)

		if err != nil {
			return nil, err
		}

		return filteredEnvironments[0], nil
	}
}

// buildPackageVersionBaseline has been shamelessly lifted from https://github.com/OctopusDeploy/cli
func (o *LiveOctopusClient) buildPackageVersionBaseline(octopus *octopusApiClient.Client, deploymentProcessTemplate *deployments.DeploymentProcessTemplate, channel *channels.Channel) ([]*octopusdeploy.SelectedPackage, error) {
	if octopus == nil {
		return nil, errors.New("octopus_apis can not be nil")
	}

	if deploymentProcessTemplate == nil {
		return nil, errors.New("deploymentProcessTemplate can not be nil")
	}

	if channel == nil {
		return nil, errors.New("channel can not be nil")
	}

	result := make([]*octopusdeploy.SelectedPackage, 0, len(deploymentProcessTemplate.Packages))

	// step 1: pass over all the packages in the deployment process, group them
	// by their feed, then subgroup by packageId

	// map(key: FeedID, value: list of references using the package so we can trace back to steps)
	feedsToQuery := make(map[string][]releases.ReleaseTemplatePackage)
	for _, pkg := range deploymentProcessTemplate.Packages {

		// If a package is not considered resolvable by the server, don't attempt to query it's feed or lookup
		// any potential versions for it; we can't succeed in that because variable templates won't get expanded
		// until deployment time
		if !pkg.IsResolvable {
			result = append(result, &octopusdeploy.SelectedPackage{
				ActionName:           pkg.ActionName,
				PackageReferenceName: pkg.PackageReferenceName,
				Version:              "",
			})
			continue
		}
		if feedPackages, seenFeedBefore := feedsToQuery[pkg.FeedID]; !seenFeedBefore {
			feedsToQuery[pkg.FeedID] = []releases.ReleaseTemplatePackage{pkg}
		} else {
			// seen both the feed and package, but not against this particular step
			feedsToQuery[pkg.FeedID] = append(feedPackages, pkg)
		}
	}

	if len(feedsToQuery) == 0 {
		return make([]*octopusdeploy.SelectedPackage, 0), nil
	}

	// step 2: load the feed resources, so we can get SearchPackageVersionsTemplate
	feedIds := make([]string, 0, len(feedsToQuery))
	for k := range feedsToQuery {
		feedIds = append(feedIds, k)
	}
	sort.Strings(feedIds) // we need to sort them otherwise the order is indeterminate. Server doesn't care but our unit tests fail
	var foundFeeds *feeds.Feeds
	err := retry.Do(
		func() error {
			var err error
			foundFeeds, err = octopus.Feeds.Get(feeds.FeedsQuery{IDs: feedIds, Take: len(feedIds)})
			return err
		}, retry_config.RetryOptions...)
	if err != nil {
		return nil, err
	}

	// step 3: for each package within a feed, ask the server to select the best package version for it, applying the channel rules
	for _, feed := range foundFeeds.Items {
		packageRefsInFeed, ok := feedsToQuery[feed.GetID()]
		if !ok {
			return nil, errors.New("internal consistency error; feed ID not found in feedsToQuery") // should never happen
		}

		cache := make(map[feeds.SearchPackageVersionsQuery]string) // cache value is the package version

		for _, packageRef := range packageRefsInFeed {
			query := feeds.SearchPackageVersionsQuery{
				PackageID: packageRef.PackageID,
				Take:      1,
			}
			// look in the channel rules for a version filter for this step+package
		rulesLoop:
			for _, rule := range channel.Rules {
				for _, ap := range rule.ActionPackages {
					if ap.PackageReference == packageRef.PackageReferenceName && ap.DeploymentAction == packageRef.ActionName {
						// this rule applies to our step/packageref combo
						query.PreReleaseTag = rule.Tag
						query.VersionRange = rule.VersionRange
						// the octopus_apis server won't let the same package be targeted by more than one rule, so
						// once we've found the first matching rule for our step+package, we can stop looping
						break rulesLoop
					}
				}
			}

			if cachedVersion, ok := cache[query]; ok {
				result = append(result, &octopusdeploy.SelectedPackage{
					ActionName:           packageRef.ActionName,
					PackageReferenceName: packageRef.PackageReferenceName,
					Version:              cachedVersion,
				})
			} else { // uncached; ask the server
				versions, err := octopus.Feeds.SearchFeedPackageVersions(feed, query)
				if err != nil {
					return nil, err
				}

				switch len(versions.Items) {
				case 0: // no package found; cache the response
					cache[query] = ""
					result = append(result, &octopusdeploy.SelectedPackage{
						ActionName:           packageRef.ActionName,
						PackageReferenceName: packageRef.PackageReferenceName,
						Version:              "",
					})

				case 1:
					cache[query] = versions.Items[0].Version
					result = append(result, &octopusdeploy.SelectedPackage{
						ActionName:           packageRef.ActionName,
						PackageReferenceName: packageRef.PackageReferenceName,
						Version:              versions.Items[0].Version,
					})

				default:
					return nil, errors.New("internal error; more than one package returned when only 1 specified")
				}
			}
		}
	}
	return result, nil
}
