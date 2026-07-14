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

func TestPatExpiryDays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		role      string
		requested int
		want      int
	}{
		{"viewer keeps no expiry", "viewer", 0, 0},
		{"viewer keeps a long expiry", "viewer", 3650, 3650},
		{"viewer keeps a short expiry", "viewer", 7, 7},

		// The "no expiry" case is the one that matters: an unbounded admin token
		// would otherwise be minted by simply omitting expires_in_days.
		{"admin no-expiry is capped", "admin", 0, maxAdminExpiresInDays},
		{"admin over-cap is capped", "admin", 3650, maxAdminExpiresInDays},
		{"admin at the cap is kept", "admin", 30, 30},
		{"admin under the cap is kept", "admin", 7, 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := patExpiryDays(tt.role, tt.requested); got != tt.want {
				t.Errorf("patExpiryDays(%q, %d) = %d, want %d", tt.role, tt.requested, got, tt.want)
			}
		})
	}
}

func TestPatMintAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		minRole string
		caller  string
		want    bool
	}{
		{"admin gate passes admin", "admin", "admin", true},
		{"admin gate blocks viewer", "admin", "viewer", false},
		{"viewer gate passes viewer", "viewer", "viewer", true},
		{"viewer gate passes admin", "viewer", "admin", true},
		{"unnormalized config fails closed for viewer", "", "viewer", false},
		{"unnormalized config still passes admin", "", "admin", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := patMintAllowed(tt.minRole, tt.caller); got != tt.want {
				t.Errorf("patMintAllowed(%q, %q) = %v, want %v", tt.minRole, tt.caller, got, tt.want)
			}
		})
	}
}
