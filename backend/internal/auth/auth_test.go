package auth

import (
	"context"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/dbulashev/dasha/internal/config"
)

const noHTTPSWarnMsg = "auth enabled without require_https — credentials may be transmitted in plaintext"

func TestNewMiddlewares_WarnsWhenAuthWithoutHTTPS(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	logger := zap.New(core)

	cfg := config.AuthConfig{
		Mode:         config.AuthModeToken,
		RequireHTTPS: false,
		Tokens: []config.AuthToken{
			{Name: "test", Token: "secret", Role: "viewer"},
		},
	}

	mw, err := NewMiddlewares(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("NewMiddlewares: %v", err)
	}
	defer mw.Stop()

	warns := logs.FilterMessage(noHTTPSWarnMsg).All()
	if len(warns) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warns))
	}

	mode, ok := warns[0].ContextMap()["auth_mode"].(string)
	if !ok || mode != "token" {
		t.Errorf("expected auth_mode=token in log fields, got %v", warns[0].ContextMap()["auth_mode"])
	}
}

func TestNewMiddlewares_NoWarnWhenAuthDisabled(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	logger := zap.New(core)

	cfg := config.AuthConfig{
		Mode:         config.AuthModeNone,
		RequireHTTPS: false,
	}

	mw, err := NewMiddlewares(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("NewMiddlewares: %v", err)
	}
	defer mw.Stop()

	if got := logs.FilterMessage(noHTTPSWarnMsg).Len(); got != 0 {
		t.Errorf("expected no warnings, got %d", got)
	}
}

func TestNewMiddlewares_NoWarnWhenAuthModeEmpty(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	logger := zap.New(core)

	cfg := config.AuthConfig{
		Mode:         "",
		RequireHTTPS: false,
	}

	mw, err := NewMiddlewares(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("NewMiddlewares: %v", err)
	}
	defer mw.Stop()

	if got := logs.FilterMessage(noHTTPSWarnMsg).Len(); got != 0 {
		t.Errorf("expected no warnings, got %d", got)
	}
}

func TestNewMiddlewares_NoWarnWhenRequireHTTPSEnabled(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	logger := zap.New(core)

	cfg := config.AuthConfig{
		Mode:         config.AuthModeToken,
		RequireHTTPS: true,
		Tokens: []config.AuthToken{
			{Name: "test", Token: "secret", Role: "viewer"},
		},
	}

	mw, err := NewMiddlewares(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("NewMiddlewares: %v", err)
	}
	defer mw.Stop()

	if got := logs.FilterMessage(noHTTPSWarnMsg).Len(); got != 0 {
		t.Errorf("expected no warnings, got %d", got)
	}
}
