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
	if cfg.InboxQueue.MaxRetry != 7 || cfg.DeliverQueue.MaxRetry != 11 {
		t.Fatalf("unexpected retry defaults: inbox=%d deliver=%d", cfg.InboxQueue.MaxRetry, cfg.DeliverQueue.MaxRetry)
	}
	if cfg.InboxQueue.Timeout != 5*time.Minute || cfg.DeliverQueue.Timeout != time.Minute {
		t.Fatalf("unexpected timeout defaults")
	}
	if cfg.InboxQueue.Concurrency != 16 || cfg.DeliverQueue.Concurrency != 128 || cfg.InboxQueue.RatePerSecond != 32 || cfg.DeliverQueue.RatePerSecond != 128 {
		t.Fatalf("unexpected queue controls: inbox=%+v deliver=%+v", cfg.InboxQueue, cfg.DeliverQueue)
	}
	if cfg.ConnectorCommandChannel != "rosmarinus:commands" {
		t.Fatalf("ConnectorCommandChannel = %q", cfg.ConnectorCommandChannel)
	}
	if cfg.ConnectorAccountEventNamespace != "rosmarinus:accounts" || cfg.ConnectorAccountControlChannel != "rosmarinus:control:accounts" {
		t.Fatalf("unexpected Connector channels: %+v", cfg)
	}
	if cfg.SalviaAccountCollection != "salvia_accounts" || cfg.ConnectorReceiptTTL != 7*24*time.Hour || cfg.ConnectorAccountReconcileInterval != 5*time.Minute {
		t.Fatalf("unexpected Salvia/receipt config: %+v", cfg)
	}
	if cfg.InboxActivityReceiptTTL != 7*24*time.Hour {
		t.Fatalf("InboxActivityReceiptTTL = %s", cfg.InboxActivityReceiptTTL)
	}
	if len(cfg.FederationBlockedHosts) != 0 {
		t.Fatalf("unexpected blocked hosts: %v", cfg.FederationBlockedHosts)
	}
	if cfg.MediaMaxBytes != 20*1024*1024 || cfg.MediaFetchTimeout != time.Minute {
		t.Fatalf("unexpected media defaults: bytes=%d timeout=%s", cfg.MediaMaxBytes, cfg.MediaFetchTimeout)
	}
	if cfg.InstanceMetadataTimeout != 30*time.Second {
		t.Fatalf("InstanceMetadataTimeout = %s", cfg.InstanceMetadataTimeout)
	}
	if cfg.SessionCookieName != "rosmarinus_session" || cfg.SessionTTL != 30*24*time.Hour || cfg.SessionSecure {
		t.Fatalf("unexpected session defaults: %+v", cfg)
	}
	if cfg.WebAuthnRPID != "localhost" || cfg.WebAuthnRPName != "Rosmarinus" || len(cfg.WebAuthnOrigins) != 1 || cfg.WebAuthnOrigins[0] != "http://localhost:3000" || cfg.WebAuthnCeremonyTTL != 5*time.Minute {
		t.Fatalf("unexpected WebAuthn defaults: %+v", cfg)
	}
}

func TestLoadSessionConfig(t *testing.T) {
	cfg, err := Load(func(key string) (string, bool) {
		switch key {
		case "PUBLIC_URL":
			return "https://social.example", true
		case "SESSION_COOKIE_NAME":
			return "salvia_session", true
		case "SESSION_TTL":
			return "12h", true
		case "WEBAUTHN_RP_ID":
			return "social.example", true
		case "WEBAUTHN_RP_NAME":
			return "Salvia", true
		case "WEBAUTHN_ALLOWED_ORIGINS":
			return "https://social.example,https://admin.social.example", true
		default:
			return "", false
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionCookieName != "salvia_session" || cfg.SessionTTL != 12*time.Hour || !cfg.SessionSecure {
		t.Fatalf("session config = %+v", cfg)
	}
	if cfg.WebAuthnRPID != "social.example" || cfg.WebAuthnRPName != "Salvia" || len(cfg.WebAuthnOrigins) != 2 {
		t.Fatalf("WebAuthn config = %+v", cfg)
	}
}

func TestLoadMediaConfig(t *testing.T) {
	cfg, err := Load(func(key string) (string, bool) {
		switch key {
		case "MEDIA_MAX_BYTES":
			return "1048576", true
		case "MEDIA_FETCH_TIMEOUT":
			return "30s", true
		case "MEDIA_ALLOWED_PRIVATE_NETWORKS":
			return "10.0.0.0/8, fd00::/8", true
		case "INSTANCE_METADATA_TIMEOUT":
			return "15s", true
		default:
			return "", false
		}
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.MediaMaxBytes != 1048576 || cfg.MediaFetchTimeout != 30*time.Second || len(cfg.MediaAllowedPrivateNetworks) != 2 || cfg.InstanceMetadataTimeout != 15*time.Second {
		t.Fatalf("unexpected media config: %+v", cfg)
	}
}

func TestLoadQueueControls(t *testing.T) {
	cfg, err := Load(func(key string) (string, bool) {
		switch key {
		case "INBOX_CONCURRENCY":
			return "8", true
		case "INBOX_RATE_PER_SECOND":
			return "24", true
		case "DELIVER_CONCURRENCY":
			return "64", true
		case "DELIVER_RATE_PER_SECOND":
			return "96", true
		default:
			return "", false
		}
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.InboxQueue.Concurrency != 8 || cfg.InboxQueue.RatePerSecond != 24 || cfg.DeliverQueue.Concurrency != 64 || cfg.DeliverQueue.RatePerSecond != 96 {
		t.Fatalf("unexpected queue controls: inbox=%+v deliver=%+v", cfg.InboxQueue, cfg.DeliverQueue)
	}
}

func TestLoadRejectsActivityReceiptTTLShorterThanLease(t *testing.T) {
	_, err := Load(func(key string) (string, bool) {
		if key == "INBOX_ACTIVITY_RECEIPT_TTL" {
			return "5m", true
		}
		return "", false
	})
	if err == nil {
		t.Fatal("short activity receipt TTL was accepted")
	}
}

func TestLoadRejectsInvalidMediaAllowedNetwork(t *testing.T) {
	_, err := Load(func(key string) (string, bool) {
		if key == "MEDIA_ALLOWED_PRIVATE_NETWORKS" {
			return "not-a-cidr", true
		}
		return "", false
	})
	if err == nil {
		t.Fatal("invalid media network was accepted")
	}
}

func TestLoadNormalizesFederationBlockedHosts(t *testing.T) {
	cfg, err := Load(func(key string) (string, bool) {
		if key == "FEDERATION_BLOCKED_HOSTS" {
			return "Bad.Example., sub.example, bad.example.", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	want := []string{"bad.example", "sub.example"}
	if len(cfg.FederationBlockedHosts) != len(want) {
		t.Fatalf("FederationBlockedHosts = %v", cfg.FederationBlockedHosts)
	}
	for i := range want {
		if cfg.FederationBlockedHosts[i] != want[i] {
			t.Fatalf("FederationBlockedHosts = %v", cfg.FederationBlockedHosts)
		}
	}
	if !cfg.IsFederationHostBlocked("social.BAD.example.") || cfg.IsFederationHostBlocked("notbad.example") {
		t.Fatalf("unexpected blocked-host matching for %v", cfg.FederationBlockedHosts)
	}
	if !cfg.IsSelfFederationURL("http://localhost:3000/users/alice") || cfg.IsSelfFederationURL("http://localhost:3001/users/alice") {
		t.Fatal("default PUBLIC_URL host should be self")
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
		case "ABLY_ROSMARINUS_API_KEY":
			return "app.key:secret", true
		case "ABLY_COMMAND_SUBSCRIBE_API_KEY":
			return "command.key:secret", true
		case "ABLY_ACCOUNT_EVENT_PUBLISH_API_KEY":
			return "event.key:secret", true
		case "ABLY_ACCOUNT_CONTROL_SUBSCRIBE_API_KEY":
			return "control.key:secret", true
		case "CONNECTOR_COMMAND_CHANNEL":
			return "test:commands", true
		case "CONNECTOR_ACCOUNT_EVENT_NAMESPACE":
			return "test:accounts", true
		case "CONNECTOR_ACCOUNT_CONTROL_CHANNEL":
			return "test:control", true
		case "SALVIA_ACCOUNT_COLLECTION":
			return "test_salvia_accounts", true
		case "CONNECTOR_RECEIPT_TTL":
			return "24h", true
		case "CONNECTOR_ACCOUNT_RECONCILE_INTERVAL":
			return "10m", true
		default:
			return "", false
		}
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.CommandSubscribeAPIKey() != "command.key:secret" || cfg.AccountEventPublishAPIKey() != "event.key:secret" || cfg.AccountControlSubscribeAPIKey() != "control.key:secret" || cfg.ConnectorCommandChannel != "test:commands" || cfg.ConnectorAccountEventNamespace != "test:accounts" || cfg.ConnectorAccountControlChannel != "test:control" || cfg.SalviaAccountCollection != "test_salvia_accounts" || cfg.ConnectorReceiptTTL != 24*time.Hour || cfg.ConnectorAccountReconcileInterval != 10*time.Minute {
		t.Fatalf("unexpected Ably config: %+v", cfg)
	}
}

func TestLoadAblyConfigFallsBackToLegacyServiceKey(t *testing.T) {
	cfg, err := Load(func(key string) (string, bool) {
		if key == "ABLY_ROSMARINUS_API_KEY" {
			return "legacy.key:secret", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.CommandSubscribeAPIKey() != "legacy.key:secret" || cfg.AccountEventPublishAPIKey() != "legacy.key:secret" || cfg.AccountControlSubscribeAPIKey() != "legacy.key:secret" {
		t.Fatalf("legacy key fallback failed: %+v", cfg)
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
