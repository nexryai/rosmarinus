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
	if cfg.BFFChannel != "rosmarinus:bff" {
		t.Fatalf("BFFChannel = %q", cfg.BFFChannel)
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

func TestLoadLocalActorConfig(t *testing.T) {
	cfg, err := Load(func(key string) (string, bool) {
		switch key {
		case "LOCAL_ACTOR_USERNAME":
			return "relay_bot", true
		case "LOCAL_ACTOR_TYPE":
			return "Service", true
		default:
			return "", false
		}
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.LocalActorUsername != "relay_bot" || cfg.LocalActorType != "Service" {
		t.Fatalf("unexpected local actor config: %+v", cfg)
	}
}

func TestLoadAblyConfig(t *testing.T) {
	cfg, err := Load(func(key string) (string, bool) {
		switch key {
		case "ABLY_API_KEY":
			return "app.key:secret", true
		case "BFF_CHANNEL":
			return "test:bff", true
		default:
			return "", false
		}
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.AblyAPIKey != "app.key:secret" || cfg.BFFChannel != "test:bff" {
		t.Fatalf("unexpected Ably config: %+v", cfg)
	}
}

func TestLoadRejectsInvalidLocalActorUsername(t *testing.T) {
	_, err := Load(func(key string) (string, bool) {
		if key == "LOCAL_ACTOR_USERNAME" {
			return ".bad", true
		}
		return "", false
	})
	if err == nil {
		t.Fatalf("expected invalid LOCAL_ACTOR_USERNAME to fail")
	}
}
