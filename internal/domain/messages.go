package domain

type ApplicationUpdateMessage struct {
	Application    string
	Namespace      string
	State          string
	TargetUrl      string
	TargetRevision string
	CommitSha      string
	Images         []string
}

type ErrorResponse struct {
	Status  string
	Message string
}
