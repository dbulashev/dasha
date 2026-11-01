package patroni

import (
	"strings"

	"github.com/dbulashev/dasha/internal/config"
)

// standbyOf maps a standby scope to the published name of the cluster it
// replicates from. scopes are the published scopes of this entry; names maps
// each of them to its cluster name. The system identifier is tried first,
// then a literal host:port match against members.
func standbyOf(scopes []scopeData, names map[string]config.ClusterName) map[string]config.ClusterName {
	out := make(map[string]config.ClusterName)

	for i := range scopes {
		s := &scopes[i]
		if !s.IsStandby {
			continue
		}

		primary, ok := bySysID(s, scopes)
		if !ok {
			primary, ok = byAddress(s, scopes)
		}

		if ok {
			out[s.Name] = names[primary]
		}
	}

	return out
}

// bySysID returns the only non-standby scope sharing s's system identifier.
func bySysID(s *scopeData, scopes []scopeData) (string, bool) {
	if s.SysID == "" {
		return "", false
	}

	found := ""

	for i := range scopes {
		c := &scopes[i]
		if c.Name == s.Name || c.IsStandby || c.SysID != s.SysID {
			continue
		}

		if found != "" {
			return "", false
		}

		found = c.Name
	}

	return found, found != ""
}

// byAddress returns the only other scope with a member at one of s's
// standby_cluster addresses.
func byAddress(s *scopeData, scopes []scopeData) (string, bool) {
	found := ""

	for _, target := range s.Standby {
		for i := range scopes {
			c := &scopes[i]
			if c.Name == s.Name || c.Name == found || !hasMemberAt(c, target) {
				continue
			}

			if found != "" {
				return "", false
			}

			found = c.Name
		}
	}

	return found, found != ""
}

func hasMemberAt(s *scopeData, target hostPort) bool {
	for _, m := range s.Members {
		if strings.EqualFold(m.Host, target.Host) && m.Port == target.Port {
			return true
		}
	}

	return false
}
