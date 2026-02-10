package dto

// InstanceInfo contains recovery status and version information for a host.
type InstanceInfo struct {
	InRecovery  bool
	VersionNum  int
	Version     string
	VersionFull string
}
