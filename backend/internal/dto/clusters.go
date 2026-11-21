package dto

import "github.com/dbulashev/dasha/internal/config"

type Instance struct {
	HostName config.Host
	Alias    string
}

type ClusterInfo struct {
	Name         config.ClusterName
	Source       string
	SupportsLogs bool
	// SupportsWordSearch means the log source runs a words-mode text filter.
	SupportsWordSearch bool
	LogStreams         []string
	LogSeverities      map[string][]string
	Instances          []Instance
	Databases          []string
	StandbyOf          string
}
