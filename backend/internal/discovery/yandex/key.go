package yandex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type authorizedKey struct {
	ID               string    `json:"id"`
	ServiceAccountID string    `json:"service_account_id"`
	CreatedAt        time.Time `json:"created_at"`
	KeyAlgorithm     string    `json:"key_algorithm"`
	PublicKey        string    `json:"public_key"`
	PrivateKey       string    `json:"private_key"` //nolint:gosec
}

func (k *authorizedKey) validate() error {
	if k.ServiceAccountID == "" {
		return fmt.Errorf("service_account_id is required")
	}

	if k.CreatedAt.IsZero() {
		return fmt.Errorf("created_at is required")
	}

	if k.KeyAlgorithm == "" {
		return fmt.Errorf("key_algorithm is required")
	}

	if k.PublicKey == "" {
		return fmt.Errorf("public_key is required")
	}

	if k.PrivateKey == "" {
		return fmt.Errorf("private_key is required")
	}

	return nil
}

func loadAuthorizedKey(filePath string) (*authorizedKey, error) {
	data, err := os.ReadFile(filepath.Clean(filePath))
	if err != nil {
		return nil, fmt.Errorf("loadAuthorizedKey | read file: %w", err)
	}

	var key authorizedKey
	if err := json.Unmarshal(data, &key); err != nil {
		return nil, fmt.Errorf("loadAuthorizedKey | parse json: %w", err)
	}

	if err := key.validate(); err != nil {
		return nil, fmt.Errorf("loadAuthorizedKey | %w", err)
	}

	return &key, nil
}
