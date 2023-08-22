package hanlders

import (
	"github.com/OctopusDeploy/go-octopusdeploy/octopusdeploy"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/models"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/domain/versioners"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/apploggers"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/octopus_apis"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/types"
	"github.com/samber/lo"
	"strings"
	"sync"
	"testing"
)

type createAndDeployReleaseDetails struct {
	project models.ArgoCDProjectExpanded
	version types.OctopusReleaseVersion
}

type mockOctopusClient struct {
	createAndDeployReleaseDetails []createAndDeployReleaseDetails
	called                        chan bool
}

func (c *mockOctopusClient) GetProjects(updateMessage models.ApplicationUpdateMessage) ([]models.ArgoCDProjectExpanded, error) {
	return []models.ArgoCDProjectExpanded{
		models.ArgoCDProjectExpanded{
			Project: &octopusdeploy.Project{
				Name: "Project 1",
			},
			Environment: &octopusdeploy.Environment{
				Name: "Development",
			},
			Channel: &octopusdeploy.Channel{
				Name: "Default",
			},
			Lifecycle: &octopusdeploy.Lifecycle{
				Name: "Default",
			},
			ReleaseVersionImage: "",
			PackageVersions:     nil,
		},
	}, nil
}

func (c *mockOctopusClient) CreateAndDeployRelease(project models.ArgoCDProjectExpanded, updateMessage models.ApplicationUpdateMessage, version types.OctopusReleaseVersion) error {
	if c.createAndDeployReleaseDetails == nil {
		c.createAndDeployReleaseDetails = []createAndDeployReleaseDetails{}
	}

	c.createAndDeployReleaseDetails = append(c.createAndDeployReleaseDetails, createAndDeployReleaseDetails{
		project: project,
		version: version,
	})

	defer func() {
		c.called <- true
	}()

	return nil
}

func (c *mockOctopusClient) GetReleaseVersions(project *octopusdeploy.Project) ([]types.OctopusReleaseVersion, error) {
	return []types.OctopusReleaseVersion{
		"0.0.1",
		"0.0.2",
	}, nil
}

func (c *mockOctopusClient) IsDeployed(project *octopusdeploy.Project, releaseVersion types.OctopusReleaseVersion, environment *octopusdeploy.Environment) (bool, error) {
	return releaseVersion == "0.0.1" || releaseVersion == "0.0.2", nil
}

func (c *mockOctopusClient) GetLatestRelease(project *octopusdeploy.Project) (*octopusdeploy.Release, error) {
	return &octopusdeploy.Release{
		Version: "0.0.2",
	}, nil
}

func (c *mockOctopusClient) GetLatestDeploymentRelease(project *octopusdeploy.Project, environment *octopusdeploy.Environment) (*octopusdeploy.Release, error) {
	if environment.Name == "Development" {
		return &octopusdeploy.Release{
			Version: "0.0.2",
		}, nil
	}

	return nil, nil
}

func createMockOctopusClient() (chan bool, octopus_apis.OctopusClient) {
	calledChannel := make(chan bool)

	client := &mockOctopusClient{
		createAndDeployReleaseDetails: nil,
		called:                        calledChannel,
	}

	return calledChannel, client
}

func createReleaseHandler(versioner versioners.ReleaseVersioner, client octopus_apis.OctopusClient) (*CreateReleaseHandler, error) {
	logger, err := apploggers.NewDevProdLogger()

	if err != nil {
		return nil, err
	}

	return &CreateReleaseHandler{
		logger:          logger,
		octo:            client,
		argo:            nil,
		versioner:       versioner,
		projectReleases: sync.Map{},
	}, nil
}

func TestNewReleaseCreation(t *testing.T) {
	calledChannel, client := createMockOctopusClient()

	handler, err := createReleaseHandler(&versioners.SimpleRedeploymentVersioner{}, client)

	if err != nil {
		t.Fatal(err)
	}

	message := models.ApplicationUpdateMessage{
		Application:    "myapplication",
		Namespace:      "development",
		State:          "success",
		TargetUrl:      "",
		TargetRevision: "0.0.3",
		CommitSha:      "abcdefghijklmnop",
		Images:         nil,
		Project:        "default",
	}

	err = handler.CreateRelease(message)

	if err != nil {
		t.Fatal(err)
	}

	<-calledChannel

	_, exists := lo.Find(handler.octo.(*mockOctopusClient).createAndDeployReleaseDetails, func(item createAndDeployReleaseDetails) bool {
		return item.project.Project.Name == "Project 1" &&
			item.project.Environment.Name == "Development" &&
			item.project.Lifecycle.Name == "Default" &&
			item.project.Channel.Name == "Default" &&
			item.project.ReleaseVersionImage == "" &&
			item.version == "0.0.3"
	})

	if !exists {
		t.Fatal("must have had a request to create a new release")
	}
}

func TestExistingReleaseCreation2(t *testing.T) {
	calledChannel, client := createMockOctopusClient()

	versioner := versioners.NewSimpleVersioner(client)

	handler, err := createReleaseHandler(&versioner, client)

	if err != nil {
		t.Fatal(err)
	}

	message := models.ApplicationUpdateMessage{
		Application:    "myapplication",
		Namespace:      "development",
		State:          "success",
		TargetUrl:      "",
		TargetRevision: "0.0.2",
		CommitSha:      "abcdefghijklmnop",
		Images:         nil,
		Project:        "default",
	}

	err = handler.CreateRelease(message)

	if err != nil {
		t.Fatal(err)
	}

	<-calledChannel

	_, exists := lo.Find(handler.octo.(*mockOctopusClient).createAndDeployReleaseDetails, func(item createAndDeployReleaseDetails) bool {
		return item.project.Project.Name == "Project 1" &&
			item.project.Environment.Name == "Development" &&
			item.project.Lifecycle.Name == "Default" &&
			item.project.Channel.Name == "Default" &&
			item.project.ReleaseVersionImage == "" &&
			// We deploy the version sent to us in the message
			strings.HasPrefix(string(item.version), "0.0.2") &&
			// But a metadata value should be present in the version to indicate a new release version
			strings.ContainsRune(string(item.version), '+')
	})

	if !exists {
		t.Fatal("must have had a request to create a new release")
	}
}

func TestExistingReleaseCreation(t *testing.T) {
	calledChannel, client := createMockOctopusClient()

	handler, err := createReleaseHandler(&versioners.SimpleRedeploymentVersioner{}, client)

	if err != nil {
		t.Fatal(err)
	}

	message := models.ApplicationUpdateMessage{
		Application:    "myapplication",
		Namespace:      "development",
		State:          "success",
		TargetUrl:      "",
		TargetRevision: "0.0.2",
		CommitSha:      "abcdefghijklmnop",
		Images:         nil,
		Project:        "default",
	}

	err = handler.CreateRelease(message)

	if err != nil {
		t.Fatal(err)
	}

	<-calledChannel

	_, exists := lo.Find(handler.octo.(*mockOctopusClient).createAndDeployReleaseDetails, func(item createAndDeployReleaseDetails) bool {
		return item.project.Project.Name == "Project 1" &&
			item.project.Environment.Name == "Development" &&
			item.project.Lifecycle.Name == "Default" &&
			item.project.Channel.Name == "Default" &&
			item.project.ReleaseVersionImage == "" &&
			item.version == "0.0.2"
	})

	if !exists {
		t.Fatal("must have had a request to create a new release")
	}
}
