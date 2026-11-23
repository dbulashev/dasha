package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPServerConfig_WithDefaults(t *testing.T) {
	got := HTTPServerConfig{}.WithDefaults() //nolint:exhaustruct

	assert.Equal(t, 30*time.Second, got.ReadTimeout)
	assert.Equal(t, 10*time.Second, got.ReadHeaderTimeout)
	assert.Equal(t, 30*time.Second, got.WriteTimeout)
	assert.Equal(t, 30*time.Second, got.IdleTimeout)
	require.NoError(t, got.Validate())
	assert.False(t, got.WriteCutsRead())

	got = HTTPServerConfig{ReadTimeout: 2 * time.Minute, WriteTimeout: 3 * time.Minute}.WithDefaults() //nolint:exhaustruct
	assert.Equal(t, 2*time.Minute, got.ReadTimeout)
	assert.Equal(t, 3*time.Minute, got.WriteTimeout)
	assert.Equal(t, DefaultHTTPServerReadHeaderTimeout, got.ReadHeaderTimeout)
	assert.Equal(t, DefaultHTTPServerIdleTimeout, got.IdleTimeout)

	neg := HTTPServerConfig{IdleTimeout: -time.Second}.WithDefaults() //nolint:exhaustruct
	assert.Equal(t, -time.Second, neg.IdleTimeout)
}

func TestHTTPServerConfig_Validate(t *testing.T) {
	base := HTTPServerConfig{}.WithDefaults() //nolint:exhaustruct

	tests := []struct {
		name    string
		mutate  func(c *HTTPServerConfig)
		wantErr string
	}{
		{"lower bound", func(c *HTTPServerConfig) { c.ReadTimeout = time.Second }, ""},
		{"upper bound", func(c *HTTPServerConfig) { c.WriteTimeout = 10 * time.Minute }, ""},
		{"read below min", func(c *HTTPServerConfig) { c.ReadTimeout = 500 * time.Millisecond }, "http_server.read_timeout"},
		{"read negative", func(c *HTTPServerConfig) { c.ReadTimeout = -time.Second }, "http_server.read_timeout"},
		{"read header above max", func(c *HTTPServerConfig) { c.ReadHeaderTimeout = time.Hour }, "http_server.read_header_timeout"},
		{"write above max", func(c *HTTPServerConfig) { c.WriteTimeout = 11 * time.Minute }, "http_server.write_timeout"},
		{"idle negative", func(c *HTTPServerConfig) { c.IdleTimeout = -time.Second }, "http_server.idle_timeout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base
			tt.mutate(&c)

			err := c.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)

				return
			}

			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestHTTPServerConfig_WriteCutsRead(t *testing.T) {
	c := HTTPServerConfig{ReadTimeout: time.Minute, WriteTimeout: 20 * time.Second}.WithDefaults() //nolint:exhaustruct
	require.NoError(t, c.Validate())
	assert.True(t, c.WriteCutsRead())

	c.WriteTimeout = time.Minute
	assert.False(t, c.WriteCutsRead())
}
