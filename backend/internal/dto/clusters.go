package dto

import "github.com/dbulashev/dasha/internal/config"

type Instance struct {
	HostName config.Host
}

type ClusterInfo struct {
	Name      config.ClusterName
	Instances []Instance
	Databases []string
}
