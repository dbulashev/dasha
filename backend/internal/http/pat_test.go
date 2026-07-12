package http

import (
	"testing"

	"github.com/dbulashev/dasha/internal/auth"
)

func TestPatSubject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		user   auth.UserContext
		want   string
		wantOK bool
	}{
		{"oidc with email", auth.UserContext{AuthMethod: auth.MethodOIDC, Email: "dba@corp", Name: "dba"}, "dba@corp", true},
		{"oidc without email rejected", auth.UserContext{AuthMethod: auth.MethodOIDC, Name: "dba"}, "", false},
		{"static token rejected", auth.UserContext{AuthMethod: auth.MethodToken, Email: "dba@corp", Name: "dba"}, "", false},
		{"pat rejected (anti-chaining)", auth.UserContext{AuthMethod: auth.MethodPAT, Name: "dba@corp"}, "", false},
		{"empty rejected", auth.UserContext{}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := patSubject(&tt.user)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("patSubject(%+v) = (%q, %v), want (%q, %v)", tt.user, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestPatRoleAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		caller    string
		requested string
		want      bool
	}{
		{"admin mints admin", "admin", "admin", true},
		{"admin mints viewer", "admin", "viewer", true},
		{"viewer mints viewer", "viewer", "viewer", true},
		{"viewer cannot mint admin", "viewer", "admin", false},
		{"unknown requested role rejected", "admin", "superuser", false},
		{"empty requested role rejected", "admin", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := patRoleAllowed(tt.caller, tt.requested); got != tt.want {
				t.Errorf("patRoleAllowed(%q, %q) = %v, want %v", tt.caller, tt.requested, got, tt.want)
			}
		})
	}
}
