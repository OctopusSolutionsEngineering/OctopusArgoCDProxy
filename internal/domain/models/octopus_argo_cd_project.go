package models

import "github.com/OctopusDeploy/go-octopusdeploy/octopusdeploy"

type ImagePackageVersion struct {
	Image            string
	PackageReference string
}

// OctopusProjectAndVars maps a project to its variables. This object is mapped to a ArgoCDProject
// as the important variables are extracted.
type OctopusProjectAndVars struct {
	Project   *octopusdeploy.Project
	Variables *octopusdeploy.VariableSet
}

// ArgoCDProject matches a project to the metadata information specified in the project's variables.
// The matching resources are only known by name at this point. This object is mapped to a ArgoCDProjectExpanded
// to reference the full Octopus resources.
type ArgoCDProject struct {
	Project             *octopusdeploy.Project
	EnvironmentName     string
	ChannelName         string
	ReleaseVersionImage string
	PackageVersions     []ImagePackageVersion
}

// ArgoCDProjectExpanded is an expanded version of ArgoCDProject, having mapped the resource names to real Octopus resources.
type ArgoCDProjectExpanded struct {
	Project             *octopusdeploy.Project
	Environment         *octopusdeploy.Environment
	Channel             *octopusdeploy.Channel
	Lifecycle           *octopusdeploy.Lifecycle
	ReleaseVersionImage string
	PackageVersions     []ImagePackageVersion
}
