package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultWorkerQueues = "inbox,deliver,system,poll-ended,media,metadata,account-delete"
)

type Config struct {
	Host      string
	PublicURL string
	HTTPAddr  string
	UserAgent string

	MongoURI      string
	MongoDatabase string

	RedisAddr     string
	RedisPassword string
	RedisDB       int

	RunHTTP      bool
	RunWorkers   bool
	WorkerQueues []string

	InboxQueue   QueueConfig
	DeliverQueue QueueConfig
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
		Host:          get(lookup, "HOST", "localhost:3000"),
		PublicURL:     get(lookup, "PUBLIC_URL", "http://localhost:3000"),
		HTTPAddr:      get(lookup, "HTTP_ADDR", ":3000"),
		MongoURI:      get(lookup, "MONGO_URI", "mongodb://localhost:27017"),
		MongoDatabase: get(lookup, "MONGO_DATABASE", "rosmarinus"),
		RedisAddr:     get(lookup, "REDIS_ADDR", "localhost:6379"),
		RedisPassword: get(lookup, "REDIS_PASSWORD", ""),
		UserAgent:     get(lookup, "USER_AGENT", "rosmarinus/0.0.1"),
		WorkerQueues:  splitCSV(get(lookup, "WORKER_QUEUES", DefaultWorkerQueues)),
		InboxQueue: QueueConfig{
			Name:          "inbox",
			MaxRetry:      getInt(lookup, "INBOX_MAX_RETRY", 10),
			Timeout:       getDuration(lookup, "INBOX_TIMEOUT", 5*time.Minute),
			RatePerSecond: getInt(lookup, "INBOX_RATE_PER_SECOND", 16),
		},
		DeliverQueue: QueueConfig{
			Name:          "deliver",
			MaxRetry:      getInt(lookup, "DELIVER_MAX_RETRY", 17),
			Timeout:       getDuration(lookup, "DELIVER_TIMEOUT", time.Minute),
			RatePerSecond: getInt(lookup, "DELIVER_RATE_PER_SECOND", 128),
		},
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
	if c.InboxQueue.MaxRetry < 0 || c.DeliverQueue.MaxRetry < 0 {
		return fmt.Errorf("queue retry counts must not be negative")
	}
	if c.InboxQueue.Timeout <= 0 || c.DeliverQueue.Timeout <= 0 {
		return fmt.Errorf("queue timeouts must be positive")
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
