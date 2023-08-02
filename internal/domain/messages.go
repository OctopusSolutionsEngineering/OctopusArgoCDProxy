package domain

type ApplicationUpdateMessage struct {
	Application string
	Namespace   string
	State       string
	TargetUrl   string
}

type ErrorResponse struct {
	Status  string
	Message string
}
