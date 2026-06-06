package http

import (
	"testing"

	"github.com/dbulashev/dasha/internal/auth"
)

func TestPatSubject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		user auth.UserContext
		want string
	}{
		{"email preferred", auth.UserContext{Email: "dba@corp", Name: "dba"}, "dba@corp"},
		{"name fallback when no email", auth.UserContext{Name: "dba"}, "dba"},
		{"empty when neither", auth.UserContext{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := patSubject(&tt.user); got != tt.want {
				t.Errorf("patSubject(%+v) = %q, want %q", tt.user, got, tt.want)
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
