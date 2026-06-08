package dto

import "github.com/dbulashev/dasha/internal/config"

type Instance struct {
	HostName config.Host
}

type ClusterInfo struct {
	Name      config.ClusterName
	Source    string
	Instances []Instance
	Databases []string
}
