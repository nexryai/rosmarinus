package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/idna"
)

const (
	DefaultWorkerQueues = "inbox,deliver,system,poll-ended,media,metadata,account-delete"
)

type Config struct {
	Host                   string
	PublicURL              string
	HTTPAddr               string
	UserAgent              string
	FederationBlockedHosts []string

	LocalActorUsername    string
	LocalActorID          string
	LocalActorDisplayName string
	LocalActorType        string

	MongoURI      string
	MongoDatabase string

	RedisAddr     string
	RedisPassword string
	RedisDB       int

	AblyServiceAPIKey                 string
	AblyCommandSubscribeAPIKey        string
	AblyAccountEventPublishAPIKey     string
	AblyAccountControlSubscribeAPIKey string
	ConnectorCommandChannel           string
	ConnectorAccountEventNamespace    string
	ConnectorAccountControlChannel    string
	SalviaAccountCollection           string
	ConnectorReceiptTTL               time.Duration
	ConnectorAccountReconcileInterval time.Duration

	RunHTTP      bool
	RunWorkers   bool
	WorkerQueues []string

	InboxQueue   QueueConfig
	DeliverQueue QueueConfig

	MediaMaxBytes               int64
	MediaFetchTimeout           time.Duration
	MediaAllowedPrivateNetworks []string
	InstanceMetadataTimeout     time.Duration
}

type QueueConfig struct {
	Name          string
	MaxRetry      int
	Timeout       time.Duration
	RatePerSecond int
}

type LookupFunc func(string) (string, bool)

func LoadFromEnv() (Config, error) {
	return Load(os.LookupEnv)
}

func Load(lookup LookupFunc) (Config, error) {
	cfg := Config{
		Host:                              get(lookup, "HOST", "localhost:3000"),
		PublicURL:                         get(lookup, "PUBLIC_URL", "http://localhost:3000"),
		HTTPAddr:                          get(lookup, "HTTP_ADDR", ":3000"),
		LocalActorUsername:                get(lookup, "LOCAL_ACTOR_USERNAME", ""),
		LocalActorID:                      get(lookup, "LOCAL_ACTOR_ID", ""),
		LocalActorDisplayName:             get(lookup, "LOCAL_ACTOR_DISPLAY_NAME", ""),
		LocalActorType:                    get(lookup, "LOCAL_ACTOR_TYPE", "Service"),
		MongoURI:                          get(lookup, "MONGO_URI", "mongodb://localhost:27017"),
		MongoDatabase:                     get(lookup, "MONGO_DATABASE", "rosmarinus"),
		RedisAddr:                         get(lookup, "REDIS_ADDR", "localhost:6379"),
		RedisPassword:                     get(lookup, "REDIS_PASSWORD", ""),
		AblyServiceAPIKey:                 get(lookup, "ABLY_ROSMARINUS_API_KEY", ""),
		AblyCommandSubscribeAPIKey:        get(lookup, "ABLY_COMMAND_SUBSCRIBE_API_KEY", ""),
		AblyAccountEventPublishAPIKey:     get(lookup, "ABLY_ACCOUNT_EVENT_PUBLISH_API_KEY", ""),
		AblyAccountControlSubscribeAPIKey: get(lookup, "ABLY_ACCOUNT_CONTROL_SUBSCRIBE_API_KEY", ""),
		ConnectorCommandChannel:           get(lookup, "CONNECTOR_COMMAND_CHANNEL", "rosmarinus:commands"),
		ConnectorAccountEventNamespace:    get(lookup, "CONNECTOR_ACCOUNT_EVENT_NAMESPACE", "rosmarinus:accounts"),
		ConnectorAccountControlChannel:    get(lookup, "CONNECTOR_ACCOUNT_CONTROL_CHANNEL", "rosmarinus:control:accounts"),
		SalviaAccountCollection:           get(lookup, "SALVIA_ACCOUNT_COLLECTION", "salvia_accounts"),
		ConnectorReceiptTTL:               getDuration(lookup, "CONNECTOR_RECEIPT_TTL", 7*24*time.Hour),
		ConnectorAccountReconcileInterval: getDuration(lookup, "CONNECTOR_ACCOUNT_RECONCILE_INTERVAL", 5*time.Minute),
		UserAgent:                         get(lookup, "USER_AGENT", "rosmarinus/0.0.1"),
		FederationBlockedHosts:            normalizeHosts(splitCSV(get(lookup, "FEDERATION_BLOCKED_HOSTS", ""))),
		WorkerQueues:                      splitCSV(get(lookup, "WORKER_QUEUES", DefaultWorkerQueues)),
		InboxQueue: QueueConfig{
			Name:          "inbox",
			MaxRetry:      getInt(lookup, "INBOX_MAX_RETRY", 7),
			Timeout:       getDuration(lookup, "INBOX_TIMEOUT", 5*time.Minute),
			RatePerSecond: getInt(lookup, "INBOX_RATE_PER_SECOND", 16),
		},
		DeliverQueue: QueueConfig{
			Name:          "deliver",
			MaxRetry:      getInt(lookup, "DELIVER_MAX_RETRY", 11),
			Timeout:       getDuration(lookup, "DELIVER_TIMEOUT", time.Minute),
			RatePerSecond: getInt(lookup, "DELIVER_RATE_PER_SECOND", 128),
		},
		MediaMaxBytes:               getInt64(lookup, "MEDIA_MAX_BYTES", 20*1024*1024),
		MediaFetchTimeout:           getDuration(lookup, "MEDIA_FETCH_TIMEOUT", time.Minute),
		MediaAllowedPrivateNetworks: splitCSV(get(lookup, "MEDIA_ALLOWED_PRIVATE_NETWORKS", "")),
		InstanceMetadataTimeout:     getDuration(lookup, "INSTANCE_METADATA_TIMEOUT", 30*time.Second),
	}

	var err error
	cfg.RedisDB, err = parseInt(get(lookup, "REDIS_DB", "0"), "REDIS_DB")
	if err != nil {
		return Config{}, err
	}
	cfg.RunHTTP, err = parseBool(get(lookup, "RUN_HTTP", "true"), "RUN_HTTP")
	if err != nil {
		return Config{}, err
	}
	cfg.RunWorkers, err = parseBool(get(lookup, "RUN_WORKERS", "true"), "RUN_WORKERS")
	if err != nil {
		return Config{}, err
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) CommandSubscribeAPIKey() string {
	return firstNonEmpty(c.AblyCommandSubscribeAPIKey, c.AblyServiceAPIKey)
}

func (c Config) AccountEventPublishAPIKey() string {
	return firstNonEmpty(c.AblyAccountEventPublishAPIKey, c.AblyServiceAPIKey)
}

func (c Config) AccountControlSubscribeAPIKey() string {
	return firstNonEmpty(c.AblyAccountControlSubscribeAPIKey, c.AblyServiceAPIKey)
}

func (c Config) IsFederationHostBlocked(host string) bool {
	host = normalizeHost(host)
	for _, blocked := range c.FederationBlockedHosts {
		blocked = normalizeHost(blocked)
		if blocked == "" {
			continue
		}
		if host == blocked || strings.HasSuffix(host, "."+blocked) {
			return true
		}
	}
	return false
}

func (c Config) IsSelfFederationURL(raw string) bool {
	publicURL, err := url.Parse(c.PublicURL)
	if err != nil || publicURL.Hostname() == "" {
		return false
	}
	targetURL, err := url.Parse(raw)
	if err != nil || targetURL.Hostname() == "" {
		return false
	}
	return normalizedAuthority(publicURL) == normalizedAuthority(targetURL)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("HOST must not be empty")
	}
	if strings.TrimSpace(c.PublicURL) == "" {
		return fmt.Errorf("PUBLIC_URL must not be empty")
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("PUBLIC_URL must be an absolute URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("PUBLIC_URL scheme must be http or https")
	}
	if strings.TrimSpace(c.MongoURI) == "" {
		return fmt.Errorf("MONGO_URI must not be empty")
	}
	if strings.TrimSpace(c.MongoDatabase) == "" {
		return fmt.Errorf("MONGO_DATABASE must not be empty")
	}
	if strings.TrimSpace(c.RedisAddr) == "" {
		return fmt.Errorf("REDIS_ADDR must not be empty")
	}
	if strings.TrimSpace(c.ConnectorCommandChannel) == "" {
		return fmt.Errorf("CONNECTOR_COMMAND_CHANNEL must not be empty")
	}
	if strings.TrimSpace(c.ConnectorAccountEventNamespace) == "" {
		return fmt.Errorf("CONNECTOR_ACCOUNT_EVENT_NAMESPACE must not be empty")
	}
	if strings.TrimSpace(c.ConnectorAccountControlChannel) == "" {
		return fmt.Errorf("CONNECTOR_ACCOUNT_CONTROL_CHANNEL must not be empty")
	}
	if strings.TrimSpace(c.SalviaAccountCollection) == "" {
		return fmt.Errorf("SALVIA_ACCOUNT_COLLECTION must not be empty")
	}
	if c.ConnectorReceiptTTL <= 0 {
		return fmt.Errorf("CONNECTOR_RECEIPT_TTL must be positive")
	}
	if c.ConnectorAccountReconcileInterval <= 0 {
		return fmt.Errorf("CONNECTOR_ACCOUNT_RECONCILE_INTERVAL must be positive")
	}
	if c.InboxQueue.MaxRetry < 0 || c.DeliverQueue.MaxRetry < 0 {
		return fmt.Errorf("queue retry counts must not be negative")
	}
	if c.InboxQueue.Timeout <= 0 || c.DeliverQueue.Timeout <= 0 {
		return fmt.Errorf("queue timeouts must be positive")
	}
	if c.MediaMaxBytes <= 0 || c.MediaFetchTimeout <= 0 {
		return fmt.Errorf("media max bytes and fetch timeout must be positive")
	}
	if c.InstanceMetadataTimeout <= 0 {
		return fmt.Errorf("INSTANCE_METADATA_TIMEOUT must be positive")
	}
	for _, network := range c.MediaAllowedPrivateNetworks {
		if _, err := netip.ParsePrefix(network); err != nil {
			return fmt.Errorf("MEDIA_ALLOWED_PRIVATE_NETWORKS contains invalid CIDR %q: %w", network, err)
		}
	}
	if c.LocalActorUsername != "" {
		if !validLocalUsername(c.LocalActorUsername) {
			return fmt.Errorf("LOCAL_ACTOR_USERNAME must match ActivityPub username rules")
		}
		switch c.LocalActorType {
		case "Person", "Service", "Application", "Group", "Organization":
		default:
			return fmt.Errorf("LOCAL_ACTOR_TYPE must be Person, Service, Application, Group, or Organization")
		}
	}
	return nil
}

func get(lookup LookupFunc, key, fallback string) string {
	if v, ok := lookup(key); ok {
		return v
	}
	return fallback
}

func getInt(lookup LookupFunc, key string, fallback int) int {
	v, ok := lookup(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getInt64(lookup LookupFunc, key string, fallback int64) int64 {
	v, ok := lookup(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func getDuration(lookup LookupFunc, key string, fallback time.Duration) time.Duration {
	v, ok := lookup(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func parseInt(value, name string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return n, nil
}

func parseBool(value, name string) (bool, error) {
	b, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", name, err)
	}
	return b, nil
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func normalizeHosts(hosts []string) []string {
	result := make([]string, 0, len(hosts))
	seen := make(map[string]struct{}, len(hosts))
	for _, host := range hosts {
		host = normalizeHost(host)
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		result = append(result, host)
	}
	return result
}

func normalizeHost(host string) string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if ascii, err := idna.Lookup.ToASCII(host); err == nil {
		return ascii
	}
	return host
}

func normalizedAuthority(value *url.URL) string {
	port := value.Port()
	if port == "" {
		switch value.Scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	return normalizeHost(value.Hostname()) + ":" + port
}

func validLocalUsername(username string) bool {
	if username == "" || len(username) > 128 {
		return false
	}
	for i, r := range username {
		ok := r == '_' || r == '-' || r == '.' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		if !ok {
			return false
		}
		if (i == 0 || i == len(username)-1) && (r == '-' || r == '.') {
			return false
		}
	}
	return true
}
