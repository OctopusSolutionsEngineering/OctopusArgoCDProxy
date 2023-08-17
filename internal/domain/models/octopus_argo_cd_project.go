package models

import "github.com/OctopusDeploy/go-octopusdeploy/octopusdeploy"

type ImagePackageVersion struct {
	Image            string
	PackageReference string
}

type ArgoCDProject struct {
	Project             *octopusdeploy.Project
	Environment         string
	Channel             string
	ReleaseVersionImage string
	PackageVersions     []ImagePackageVersion
}
