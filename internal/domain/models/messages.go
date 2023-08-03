package models

type ApplicationUpdateMessage struct {
	Application    string
	Namespace      string
	State          string
	TargetUrl      string
	TargetRevision string
	CommitSha      string
	Images         []string
	Project        string
}

type ErrorResponse struct {
	Status  string
	Message string
}
