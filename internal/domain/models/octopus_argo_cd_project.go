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
// This object is mapped to a ArgoCDProjectExpanded.
type ArgoCDProject struct {
	Project             *octopusdeploy.Project
	EnvironmentName     string
	ChannelName         string
	ReleaseVersionImage string
	PackageVersions     []ImagePackageVersion
}

// ArgoCDProjectExpanded matches a project to the resources identified in a ArgoCDProject object.
type ArgoCDProjectExpanded struct {
	Project             *octopusdeploy.Project
	Environment         *octopusdeploy.Environment
	Channel             *octopusdeploy.Channel
	Lifecycle           *octopusdeploy.Lifecycle
	ReleaseVersionImage string
	PackageVersions     []ImagePackageVersion
}
