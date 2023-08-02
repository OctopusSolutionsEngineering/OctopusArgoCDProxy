package domain

type ApplicationUpdateMessage struct {
	Application string
	Namespace   string
	State       string
	TargetUrl   string
}
