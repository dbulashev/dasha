package yandex

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

// writeTestKey writes an authorized key that passes loadAuthorizedKey's checks.
// Nothing here is parsed as a real key: the SDK is only built on first API call.
func writeTestKey(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "key.json")
	body := `{"id":"ajeexample","service_account_id":"aje1example",` +
		`"created_at":"2026-01-01T00:00:00Z","key_algorithm":"RSA_2048",` +
		`"public_key":"public","private_key":"private"}`

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	return path
}

func TestNewProviderRequiresCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     map[string]any
		wantErr error
	}{
		{
			name:    "no authorized key",
			raw:     map[string]any{"folder_id": "b1g123"},
			wantErr: errAuthorizedKeyRequired,
		},
		{
			name:    "no folder id",
			raw:     map[string]any{"authorized_key": "/etc/dasha/key.json"},
			wantErr: errFolderIDRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewProvider("folder", tt.raw, nil, false, zap.NewNop())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewProvider() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewProviderInvalidFilter(t *testing.T) {
	t.Parallel()

	// Filters are compiled before the SDK is built, so a bad regex is reported
	// even though the key file below does not exist.
	_, err := NewProvider("folder", map[string]any{
		"authorized_key": "/nonexistent/key.json",
		"folder_id":      "b1g123",
		"clusters":       []any{map[string]any{"name": "["}},
	}, nil, false, zap.NewNop())

	if err == nil || !strings.Contains(err.Error(), "compile filter") {
		t.Fatalf("NewProvider() error = %v, want a filter compile error", err)
	}
}

func TestNewProviderMissingKeyFile(t *testing.T) {
	t.Parallel()

	_, err := NewProvider("folder", map[string]any{
		"authorized_key": "/nonexistent/key.json",
		"folder_id":      "b1g123",
	}, nil, false, zap.NewNop())

	if err == nil || !strings.Contains(err.Error(), "init yandex sdk") {
		t.Fatalf("NewProvider() error = %v, want an sdk init error", err)
	}
}

func TestNewProviderInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  map[string]any
		want time.Duration
	}{
		{
			name: "explicit interval",
			raw:  map[string]any{"refresh_interval": 7},
			want: 7 * time.Minute,
		},
		{
			name: "unset falls back to default",
			raw:  map[string]any{},
			want: defaultRefreshInterval,
		},
		{
			name: "non-positive falls back to default",
			raw:  map[string]any{"refresh_interval": -1},
			want: defaultRefreshInterval,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw := tt.raw
			raw["authorized_key"] = writeTestKey(t)
			raw["folder_id"] = "b1g123"

			p, err := NewProvider("folder", raw, nil, false, zap.NewNop())
			if err != nil {
				t.Fatalf("NewProvider() error = %v", err)
			}

			if got := p.Interval(); got != tt.want {
				t.Errorf("Interval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewProviderResolvesPasswordAndRegistersSDK(t *testing.T) {
	t.Setenv("DASHA_TEST_YC_PASSWORD", "s3cret")

	registry := NewRegistry()

	p, err := NewProvider("prod_folder", map[string]any{
		"authorized_key":    writeTestKey(t),
		"folder_id":         "b1g123",
		"user":              "monitoring",
		"password":          "ignored",
		"password_from_env": "DASHA_TEST_YC_PASSWORD",
	}, registry, true, zap.NewNop())
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	if p.password != "s3cret" {
		t.Errorf("password = %q, want the value from the environment", p.password)
	}

	if p.username != "monitoring" || !p.prefixName {
		t.Errorf("got username %q, prefixName %v", p.username, p.prefixName)
	}

	if _, ok := registry.Get("b1g123"); !ok {
		t.Error("SDK was not registered for folder b1g123")
	}
}
