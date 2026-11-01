package patroni

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	errCertKeyPair = errors.New("tls.cert_file and tls.key_file must be set together")
	errNoCACerts   = errors.New("no certificates found in tls.ca_file")
)

// TLSConfig is the client TLS of a DCS connection.
type TLSConfig struct {
	CAFile             string `mapstructure:"ca_file"`
	CertFile           string `mapstructure:"cert_file"`
	KeyFile            string `mapstructure:"key_file"`
	InsecureSkipVerify bool   `mapstructure:"insecure_skip_verify"`
}

func (c TLSConfig) isSet() bool {
	return c != TLSConfig{} //nolint:exhaustruct
}

// build returns a client tls.Config; without ca_file the system roots are used.
func (c TLSConfig) build() (*tls.Config, error) {
	cfg := &tls.Config{ //nolint:exhaustruct
		MinVersion: tls.VersionTLS12,
		// #nosec G402 -- opting out of verification is an explicit operator choice.
		InsecureSkipVerify: c.InsecureSkipVerify,
	}

	if c.CAFile != "" {
		pem, err := os.ReadFile(filepath.Clean(c.CAFile))
		if err != nil {
			return nil, fmt.Errorf("read tls.ca_file: %w", err)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%w: %s", errNoCACerts, c.CAFile)
		}

		cfg.RootCAs = pool
	}

	if (c.CertFile == "") != (c.KeyFile == "") {
		return nil, errCertKeyPair
	}

	if c.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load tls client certificate: %w", err)
		}

		cfg.Certificates = []tls.Certificate{cert}
	}

	return cfg, nil
}
