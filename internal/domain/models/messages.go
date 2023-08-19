package models

// ApplicationUpdateMessage is the message sent by the ArgoCD notification service
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

// ErrorResponse is the response sent to the client if there was an error
type ErrorResponse struct {
	Status  string
	Message string
}
