package models

import "github.com/OctopusDeploy/go-octopusdeploy/octopusdeploy"

type ArgoCDProject struct {
	Project             *octopusdeploy.Project
	Environment         string
	ReleaseVersionImage string
}
