// Package yandex implements Yandex Cloud MDB PostgreSQL discovery client.
package yandex

import (
	"context"
	"fmt"
	"sync"

	"github.com/yandex-cloud/go-genproto/yandex/cloud/iam/v1"
	ycsdk "github.com/yandex-cloud/go-sdk"
	"github.com/yandex-cloud/go-sdk/iamkey"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SDK wraps the Yandex Cloud SDK with lazy initialization.
type SDK struct {
	mu    sync.Mutex
	key   *authorizedKey
	ycsdk *ycsdk.SDK
}

// NewSDK loads an authorized key from a JSON file and returns an SDK.
func NewSDK(jsonFilePath string) (*SDK, error) {
	key, err := loadAuthorizedKey(jsonFilePath)
	if err != nil {
		return nil, fmt.Errorf("NewSDK | %w", err)
	}

	return &SDK{key: key}, nil //nolint:exhaustruct
}

func (sdk *SDK) build(ctx context.Context) (*ycsdk.SDK, error) {
	credentials, err := ycsdk.ServiceAccountKey(
		&iamkey.Key{ //nolint:exhaustruct
			Id:           sdk.key.ID,
			CreatedAt:    timestamppb.New(sdk.key.CreatedAt),
			KeyAlgorithm: iam.Key_Algorithm(iam.Key_Algorithm_value[sdk.key.KeyAlgorithm]),
			PublicKey:    sdk.key.PublicKey,
			PrivateKey:   sdk.key.PrivateKey,
			Subject:      &iamkey.Key_ServiceAccountId{ServiceAccountId: sdk.key.ServiceAccountID},
		})
	if err != nil {
		return nil, fmt.Errorf("build | service account key: %w", err)
	}

	y, err := ycsdk.Build(ctx, ycsdk.Config{Credentials: credentials}) //nolint:exhaustruct
	if err != nil {
		return nil, fmt.Errorf("build | sdk build: %w", err)
	}

	return y, nil
}

// Client returns the underlying Yandex SDK, building it lazily on first call.
func (sdk *SDK) Client(ctx context.Context) (*ycsdk.SDK, error) {
	sdk.mu.Lock()
	defer sdk.mu.Unlock()

	if sdk.ycsdk == nil {
		var err error

		sdk.ycsdk, err = sdk.build(ctx)
		if err != nil {
			return nil, err
		}
	}

	return sdk.ycsdk, nil
}
