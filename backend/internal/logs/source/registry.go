package source

import (
	"github.com/dbulashev/dasha/internal/config"
)

// Registry resolves a cluster to the log source serving it. Providers are
// registered at startup, so lookup is read-only.
type Registry struct {
	names       []string
	providers   map[string]Provider
	defaultName string
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		names:       nil,
		providers:   map[string]Provider{},
		defaultName: "",
	}
}

// Register adds a named provider, replacing any provider under that name.
func (r *Registry) Register(name string, p Provider) {
	if _, exists := r.providers[name]; !exists {
		r.names = append(r.names, name)
	}

	r.providers[name] = p
}

// SetDefault names the source used by clusters that do not name one.
func (r *Registry) SetDefault(name string) {
	r.defaultName = name
}

// For returns the provider serving the cluster and the source name.
// Resolution order: the cluster's own source, then any provider that claims the
// cluster by its shape, then the configured default. A provider that claims a
// cluster reads logs the default source does not hold, so it wins over the
// fleet-wide fallback.
func (r *Registry) For(cluster config.Cluster) (Provider, string, bool) {
	if name := cluster.LogSource; name != "" {
		p, ok := r.providers[name]

		return p, name, ok
	}

	for _, name := range r.names {
		m, ok := r.providers[name].(ClusterMatcher)
		if ok && m.Matches(cluster) {
			return r.providers[name], name, true
		}
	}

	if r.defaultName != "" {
		if p, ok := r.providers[r.defaultName]; ok {
			return p, r.defaultName, true
		}
	}

	return nil, "", false
}

// Supports reports whether any source serves the cluster.
func (r *Registry) Supports(cluster config.Cluster) bool {
	_, _, ok := r.For(cluster)

	return ok
}

// Streams lists the streams the cluster's source serves.
func (r *Registry) Streams(cluster config.Cluster) []string {
	p, _, ok := r.For(cluster)
	if !ok {
		return nil
	}

	return p.Streams()
}
