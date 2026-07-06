package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Host != "localhost:3000" {
		t.Fatalf("Host = %q", cfg.Host)
	}
	if cfg.PublicURL != "http://localhost:3000" {
		t.Fatalf("PublicURL = %q", cfg.PublicURL)
	}
	if !cfg.RunHTTP || !cfg.RunWorkers {
		t.Fatalf("default run flags should be true")
	}
	if cfg.InboxQueue.MaxRetry != 10 || cfg.DeliverQueue.MaxRetry != 17 {
		t.Fatalf("unexpected retry defaults: inbox=%d deliver=%d", cfg.InboxQueue.MaxRetry, cfg.DeliverQueue.MaxRetry)
	}
	if cfg.InboxQueue.Timeout != 5*time.Minute || cfg.DeliverQueue.Timeout != time.Minute {
		t.Fatalf("unexpected timeout defaults")
	}
}

func TestLoadRejectsInvalidPublicURL(t *testing.T) {
	_, err := Load(func(key string) (string, bool) {
		if key == "PUBLIC_URL" {
			return "not a url", true
		}
		return "", false
	})
	if err == nil {
		t.Fatalf("expected invalid PUBLIC_URL to fail")
	}
}

func TestLoadRejectsEmptyRequiredValues(t *testing.T) {
	_, err := Load(func(key string) (string, bool) {
		if key == "HOST" {
			return "", true
		}
		return "", false
	})
	if err == nil {
		t.Fatalf("expected empty HOST to fail")
	}
}
