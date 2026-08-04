package config

import (
	"slices"
	"testing"
)

func newTestClusters(static ...string) Clusters {
	cls := make([]Cluster, 0, len(static))
	for _, name := range static {
		cls = append(cls, Cluster{Name: ClusterName(name)}) //nolint:exhaustruct
	}

	return NewClustersFromConfig(Config{Clusters: cls}) //nolint:exhaustruct
}

func discoveredClusters(names ...string) []Cluster {
	cls := make([]Cluster, 0, len(names))
	for _, name := range names {
		cls = append(cls, Cluster{ //nolint:exhaustruct
			Name:   ClusterName(name),
			Source: SourceYandexMDB,
		})
	}

	return cls
}

func clusterNames(t *testing.T, c Clusters) []string {
	t.Helper()

	all, err := c.Get(t.Context())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	names := make([]string, 0, len(all))
	for _, cl := range all {
		names = append(names, cl.Name.String())
	}

	return names
}

func TestUpdateSourceKeepsOtherSources(t *testing.T) {
	t.Parallel()

	c := newTestClusters("local")

	c.UpdateSource("folder_a", discoveredClusters("a1"))
	c.UpdateSource("folder_b", discoveredClusters("b1"))

	if got, want := clusterNames(t, c), []string{"local", "a1", "b1"}; !slices.Equal(got, want) {
		t.Fatalf("after two sources: got %v, want %v", got, want)
	}

	// A refresh of one source must not drop what the other published.
	c.UpdateSource("folder_a", discoveredClusters("a2"))

	if got, want := clusterNames(t, c), []string{"local", "a2", "b1"}; !slices.Equal(got, want) {
		t.Fatalf("after refresh of folder_a: got %v, want %v", got, want)
	}
}

func TestUpdateSourceEmptyClearsOwnClustersOnly(t *testing.T) {
	t.Parallel()

	c := newTestClusters("local")

	c.UpdateSource("folder_a", discoveredClusters("a1"))
	c.UpdateSource("folder_b", discoveredClusters("b1"))
	c.UpdateSource("folder_a", nil)

	if got, want := clusterNames(t, c), []string{"local", "b1"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestUpdateSourceRejectsTakenNames(t *testing.T) {
	t.Parallel()

	c := newTestClusters("local")

	rejected := c.UpdateSource("folder_a", discoveredClusters("local", "x"))
	if want := []string{"local"}; !slices.Equal(rejected, want) {
		t.Fatalf("static name collision: rejected %v, want %v", rejected, want)
	}

	rejected = c.UpdateSource("folder_b", discoveredClusters("x", "y"))
	if want := []string{"x"}; !slices.Equal(rejected, want) {
		t.Fatalf("cross-source collision: rejected %v, want %v", rejected, want)
	}

	rejected = c.UpdateSource("folder_c", discoveredClusters("z", "z"))
	if want := []string{"z"}; !slices.Equal(rejected, want) {
		t.Fatalf("duplicate within one batch: rejected %v, want %v", rejected, want)
	}

	if got, want := clusterNames(t, c), []string{"local", "x", "y", "z"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestUpdateSourceReclaimsOwnNames(t *testing.T) {
	t.Parallel()

	c := newTestClusters()

	c.UpdateSource("folder_a", discoveredClusters("a1"))

	// Republishing the same name from the same source is a refresh, not a clash.
	if rejected := c.UpdateSource("folder_a", discoveredClusters("a1")); len(rejected) > 0 {
		t.Fatalf("own name rejected: %v", rejected)
	}

	if got, want := clusterNames(t, c), []string{"a1"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestGetOrderIsStable(t *testing.T) {
	t.Parallel()

	c := newTestClusters("local")

	// Published out of order: Get sorts by source name, so the API sees a stable
	// list regardless of map iteration order.
	c.UpdateSource("zeta", discoveredClusters("z1"))
	c.UpdateSource("alpha", discoveredClusters("a1"))
	c.UpdateSource("mu", discoveredClusters("m1"))

	want := []string{"local", "a1", "m1", "z1"}
	for range 10 {
		if got := clusterNames(t, c); !slices.Equal(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestStaticClustersGetDefaultSource(t *testing.T) {
	t.Parallel()

	c := newTestClusters("local")

	all, err := c.Get(t.Context())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if len(all) != 1 || all[0].Source != "static" {
		t.Fatalf("got %+v, want one cluster with source static", all)
	}
}
