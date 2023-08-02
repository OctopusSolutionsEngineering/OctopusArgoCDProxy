package octopus

type OctopusClient interface {
	CreateAndDeployRelease(application string, namespace string) error
}
