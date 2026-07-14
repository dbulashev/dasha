package http

import (
	"testing"

	"github.com/dbulashev/dasha/internal/auth"
)

func TestPatAdmin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		user *auth.UserContext
		want bool
	}{
		{"oidc admin", &auth.UserContext{AuthMethod: auth.MethodOIDC, Email: "dba@corp", Role: "admin"}, true},
		{"oidc viewer rejected", &auth.UserContext{AuthMethod: auth.MethodOIDC, Email: "dev@corp", Role: "viewer"}, false},
		{"admin pat rejected (anti-chaining)", &auth.UserContext{AuthMethod: auth.MethodPAT, Role: "admin"}, false},
		{"static admin token rejected", &auth.UserContext{AuthMethod: auth.MethodToken, Role: "admin"}, false},
		{"unknown role rejected", &auth.UserContext{AuthMethod: auth.MethodOIDC, Email: "x@corp", Role: "superuser"}, false},
		{"nil rejected", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := patAdmin(tt.user); got != tt.want {
				t.Errorf("patAdmin(%+v) = %v, want %v", tt.user, got, tt.want)
			}
		})
	}
}
