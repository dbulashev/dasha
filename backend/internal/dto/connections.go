package dto

type ConnectionSources struct {
	Database         string
	Username         string
	ApplicationName  string
	ClientAddr       string
	TotalConnections int64
}

type ConnectionStates struct {
	State string
	Count int64
}

type WaitEvent struct {
	WaitEventType string
	WaitEvent     string
	Count         int64
}

type ConnectionStatActivity struct {
	Pid             int
	Database        string
	UserName        string
	ApplicationName string
	ClientAddr      string
	State           string
	Ssl             bool
	BackendType     string
}
