package argocd_apis

import (
	"context"
	"errors"
	"github.com/OctopusSolutionsEngineering/OctopusArgoCDProxy/internal/infrastructure/retry_config"
	"github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/avast/retry-go"
	"os"

	"github.com/argoproj/argo-cd/v2/pkg/apiclient/cluster"

	"github.com/argoproj/argo-cd/v2/pkg/apiclient"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/application"
	"github.com/argoproj/argo-cd/v2/pkg/apiclient/project"
)

// ArgoCDClient provides access to the ArgoCD API
type ArgoCDClient struct {
	projectClient     project.ProjectServiceClient
	clusterClient     cluster.ClusterServiceClient
	applicationClient application.ApplicationServiceClient
}

func NewClient() (*ArgoCDClient, error) {
	if os.Getenv("ARGOCD_SERVER") == "" {
		return nil, errors.New("ARGOCD_SERVER must be defined")
	}

	if os.Getenv("ARGOCD_TOKEN") == "" {
		return nil, errors.New("ARGOCD_TOKEN must be defined")
	}

	apiClient, err := apiclient.NewClient(&apiclient.ClientOptions{
		ServerAddr: os.Getenv("ARGOCD_SERVER"),
		Insecure:   true,
		AuthToken:  os.Getenv("ARGOCD_TOKEN"),
	})
	if err != nil {
		return nil, err
	}

	_, projectClient, err := apiClient.NewProjectClient()
	if err != nil {
		return nil, err
	}

	_, clusterClient, err := apiClient.NewClusterClient()
	if err != nil {
		return nil, err
	}

	_, applicationClient, err := apiClient.NewApplicationClient()
	if err != nil {
		return nil, err
	}

	return &ArgoCDClient{
		projectClient:     projectClient,
		clusterClient:     clusterClient,
		applicationClient: applicationClient,
	}, nil
}

func (c *ArgoCDClient) GetClusters() ([]v1alpha1.Cluster, error) {
	var cl *v1alpha1.ClusterList
	err := retry.Do(
		func() error {
			var err error
			cl, err = c.clusterClient.List(context.Background(), &cluster.ClusterQuery{})
			return err
		}, retry_config.RetryOptions...)
	if err != nil {
		return nil, err
	}

	return cl.Items, nil
}

func (c *ArgoCDClient) GetProject(name string) (*v1alpha1.AppProject, error) {
	var appProject *v1alpha1.AppProject
	err := retry.Do(
		func() error {
			var err error
			appProject, err = c.projectClient.Get(context.Background(), &project.ProjectQuery{
				Name: name,
			})
			return err
		})

	return appProject, err
}

func (c *ArgoCDClient) GetApplication(name string, namespace string) (*v1alpha1.Application, error) {
	var argoApplication *v1alpha1.Application
	err := retry.Do(
		func() error {
			var err error
			argoApplication, err = c.applicationClient.Get(context.Background(), &application.ApplicationQuery{
				Name:         &name,
				AppNamespace: &namespace,
			})
			return err
		}, retry_config.RetryOptions...)

	return argoApplication, err
}

func (c *ArgoCDClient) GetApplicationResourceTree(name string, namespace string) (*v1alpha1.ApplicationTree, error) {
	var resourceTree *v1alpha1.ApplicationTree
	err := retry.Do(
		func() error {
			var err error
			resourceTree, err = c.applicationClient.ResourceTree(context.Background(), &application.ResourcesQuery{
				ApplicationName: &name,
				AppNamespace:    &namespace,
			})
			return err
		}, retry_config.RetryOptions...)
	return resourceTree, err
}
