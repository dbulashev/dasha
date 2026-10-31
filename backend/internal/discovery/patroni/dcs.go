package patroni

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

const dcsTimeout = 10 * time.Second

var errConsulNotImplemented = errors.New("dcs consul is not supported yet")

type kv struct {
	Key   string
	Value []byte
}

// store reads one key prefix from a DCS.
type store interface {
	// list returns every key under prefix in a single request. A prefix with no
	// keys is an empty result, not an error.
	list(ctx context.Context, prefix string) ([]kv, error)
	Close() error
}

func newStore(cfg Config, logger *zap.Logger) (store, error) {
	switch cfg.DCS {
	case dcsEtcd3:
		return newEtcdStore(cfg.Etcd3, logger)
	case dcsConsul:
		return nil, errConsulNotImplemented
	default:
		return nil, fmt.Errorf("%w: %q", errDCSUnknown, cfg.DCS)
	}
}

// normalizeNamespace collapses slashes the way Patroni builds its base path:
// "service", "/service/" and "//service" all give "/service"; "/" stays "/".
func normalizeNamespace(ns string) string {
	parts := strings.FieldsFunc(ns, func(r rune) bool { return r == '/' })

	return "/" + strings.Join(parts, "/")
}

// namespacePrefix returns the key prefix of all scopes: "/service/" for etcd,
// "service/" for Consul, whose keys have no leading slash. The trailing slash
// keeps "/service" from matching "/service2".
func namespacePrefix(ns, dcs string) string {
	prefix := normalizeNamespace(ns)
	if prefix != "/" {
		prefix += "/"
	}

	if dcs == dcsConsul {
		prefix = strings.TrimPrefix(prefix, "/")
	}

	return prefix
}

// usesHTTPS reports whether any endpoint asks for TLS by its scheme.
func usesHTTPS(endpoints []string) bool {
	for _, e := range endpoints {
		if u, err := url.Parse(e); err == nil && strings.EqualFold(u.Scheme, "https") {
			return true
		}
	}

	return false
}
