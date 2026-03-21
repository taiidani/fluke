package config

import (
	"fmt"
	"os"
	"time"

	"github.com/hashicorp/hcl/v2/hclsimple"
)

// ── Raw HCL structs (decode targets) ─────────────────────────────────────────
// These mirror the HCL file structure exactly. All string fields for durations
// are kept as strings and resolved in resolveServer. Environment fallback is
// applied only for agent_tokens via FLUKE_AGENT_TOKENS.

type rawServerFile struct {
	Server rawServerBlock `hcl:"server,block"`
}

type rawServerBlock struct {
	ListenGRPC  string   `hcl:"listen_grpc,optional"`
	ListenHTTP  string   `hcl:"listen_http,optional"`
	AgentTokens []string `hcl:"agent_tokens,optional"`

	Git        []rawGitBlock        `hcl:"git,block"`
	TLS        []rawServerTLS       `hcl:"tls,block"`
	Drift      []rawDriftBlock      `hcl:"drift,block"`
	EventStore []rawEventStoreBlock `hcl:"event_store,block"`
	Log        []rawLogBlock        `hcl:"log,block"`
}

type rawGitBlock struct {
	URL               string `hcl:"url,attr"`
	Branch            string `hcl:"branch,optional"`
	PollInterval      string `hcl:"poll_interval,optional"`
	ManifestGlob      string `hcl:"manifest_glob,optional"`
	SSHKeyFile        string `hcl:"ssh_key_file,optional"`
	BasicAuthUser     string `hcl:"basic_auth_user,optional"`
	BasicAuthPassword string `hcl:"basic_auth_password,optional"`
}

type rawServerTLS struct {
	Enabled  *bool  `hcl:"enabled,optional"`
	CertFile string `hcl:"cert_file,optional"`
	KeyFile  string `hcl:"key_file,optional"`
}

type rawEventStoreBlock struct {
	Backend string                `hcl:"backend,optional"`
	Memory  []rawEventStoreMemory `hcl:"memory,block"`
	Redis   []rawEventStoreRedis  `hcl:"redis,block"`
}

type rawEventStoreMemory struct {
	MaxEventsPerAgent int `hcl:"max_events_per_agent,optional"`
}

type rawEventStoreRedis struct {
	URL    string `hcl:"url,optional"`
	Prefix string `hcl:"prefix,optional"`
	TTL    string `hcl:"ttl,optional"`
}

type rawDriftBlock struct {
	Policy       string `hcl:"policy,optional"`
	AlertWebhook string `hcl:"alert_webhook,optional"`
}

type rawLogBlock struct {
	Level  string `hcl:"level,optional"`
	Format string `hcl:"format,optional"`
}

// ── Resolved config types ─────────────────────────────────────────────────────

// ServerConfig is the fully parsed and defaulted server configuration.
// All duration strings are parsed, token fallback is applied for agent_tokens,
// and default values are applied.
type ServerConfig struct {
	ListenGRPC  string
	ListenHTTP  string
	AgentTokens []string
	Git         GitConfig
	TLS         ServerTLSConfig
	Drift       DriftConfig
	EventStore  EventStoreConfig
	Log         LogConfig
}

// GitConfig holds the resolved Git repository settings.
type GitConfig struct {
	URL               string
	Branch            string
	PollInterval      time.Duration
	ManifestGlob      string
	SSHKeyFile        string
	BasicAuthUser     string
	BasicAuthPassword string
}

// ServerTLSConfig holds TLS settings for the server's gRPC listener.
// TLS is enabled when CertFile and KeyFile are both non-empty.
// Leave both empty to run without TLS (development only).
type ServerTLSConfig struct {
	Enabled  bool
	CertFile string
	KeyFile  string
}

// TLSEnabled reports whether both certificate files are configured.
func (t ServerTLSConfig) TLSEnabled() bool {
	return t.Enabled && t.CertFile != "" && t.KeyFile != ""
}

// DriftConfig holds the server's drift detection settings.
type DriftConfig struct {
	Policy       string // remediate | remediate_and_alert | alert_only
	AlertWebhook string // required for non-remediate policies
}

type EventStoreConfig struct {
	Backend string
	Memory  EventStoreMemoryConfig
	Redis   EventStoreRedisConfig
}

type EventStoreMemoryConfig struct {
	MaxEventsPerAgent int
}

type EventStoreRedisConfig struct {
	URL    string
	Prefix string
	TTL    time.Duration
}

// ── Loader ────────────────────────────────────────────────────────────────────

// LoadServer parses a server HCL config file and returns a fully resolved
// ServerConfig with defaults applied and token env fallback resolution.
func LoadServer(path string) (*ServerConfig, error) {
	var raw rawServerFile
	if err := hclsimple.DecodeFile(path, nil, &raw); err != nil {
		return nil, fmt.Errorf("parse server config %q: %w", path, err)
	}
	return resolveServer(raw.Server)
}

func resolveServer(r rawServerBlock) (*ServerConfig, error) {
	agentTokens := trimmedNonEmptyStrings(r.AgentTokens)
	if len(agentTokens) == 0 {
		agentTokens = trimmedCSVNonEmpty(os.Getenv("FLUKE_AGENT_TOKENS"))
	}

	cfg := &ServerConfig{
		ListenGRPC:  coalesce(r.ListenGRPC, ":7070"),
		ListenHTTP:  coalesce(r.ListenHTTP, ":7071"),
		AgentTokens: agentTokens,
		TLS:         ServerTLSConfig{Enabled: true},
	}
	if len(cfg.AgentTokens) == 0 {
		return nil, fmt.Errorf("agent_tokens is required")
	}

	// git block — required for the server to do anything useful, but not
	// enforced here so the binary can start and accept connections without it.
	if len(r.Git) > 0 {
		g := r.Git[0]
		url := g.URL
		if url == "" {
			return nil, fmt.Errorf("git.url is required when git block is present")
		}
		pollInterval, err := parseDuration(g.PollInterval, 60*time.Second)
		if err != nil {
			return nil, fmt.Errorf("invalid git.poll_interval %q: %w", g.PollInterval, err)
		}
		cfg.Git = GitConfig{
			URL:               url,
			Branch:            coalesce(g.Branch, "main"),
			PollInterval:      pollInterval,
			ManifestGlob:      coalesce(g.ManifestGlob, "**/*.fluke.hcl"),
			SSHKeyFile:        g.SSHKeyFile,
			BasicAuthUser:     g.BasicAuthUser,
			BasicAuthPassword: g.BasicAuthPassword,
		}
	}

	if len(r.TLS) > 0 {
		t := r.TLS[0]
		enabled := true
		if t.Enabled != nil {
			enabled = *t.Enabled
		}
		cfg.TLS = ServerTLSConfig{
			Enabled:  enabled,
			CertFile: t.CertFile,
			KeyFile:  t.KeyFile,
		}
		if err := validateServerTLSConfig(cfg.TLS); err != nil {
			return nil, err
		}
	}

	cfg.Drift = DriftConfig{Policy: "remediate"}
	if len(r.Drift) > 0 {
		d := r.Drift[0]
		cfg.Drift = DriftConfig{
			Policy:       coalesce(d.Policy, "remediate"),
			AlertWebhook: d.AlertWebhook,
		}
	}
	if err := validateDriftConfig(cfg.Drift); err != nil {
		return nil, err
	}

	cfg.EventStore = EventStoreConfig{
		Backend: "memory",
		Memory: EventStoreMemoryConfig{
			MaxEventsPerAgent: 100,
		},
		Redis: EventStoreRedisConfig{
			Prefix: "fluke",
			TTL:    168 * time.Hour,
		},
	}
	if len(r.EventStore) > 0 {
		e := r.EventStore[0]
		cfg.EventStore.Backend = coalesce(e.Backend, "memory")

		if len(e.Memory) > 0 {
			cfg.EventStore.Memory.MaxEventsPerAgent = e.Memory[0].MaxEventsPerAgent
			if cfg.EventStore.Memory.MaxEventsPerAgent == 0 {
				cfg.EventStore.Memory.MaxEventsPerAgent = 100
			}
		}

		if len(e.Redis) > 0 {
			rd := e.Redis[0]
			cfg.EventStore.Redis.URL = rd.URL
			cfg.EventStore.Redis.Prefix = coalesce(rd.Prefix, "fluke")
			ttl, err := parseDuration(rd.TTL, 168*time.Hour)
			if err != nil {
				return nil, fmt.Errorf("invalid event_store.redis.ttl %q: %w", rd.TTL, err)
			}
			cfg.EventStore.Redis.TTL = ttl
		}
	}
	if err := validateEventStoreConfig(cfg.EventStore); err != nil {
		return nil, err
	}

	cfg.Log = LogConfig{Level: "info", Format: "text"}
	if len(r.Log) > 0 {
		l := r.Log[0]
		cfg.Log = LogConfig{
			Level:  coalesce(l.Level, "info"),
			Format: coalesce(l.Format, "text"),
		}
	}
	if err := validateLogConfig(cfg.Log); err != nil {
		return nil, err
	}

	return cfg, nil
}

func validateDriftConfig(cfg DriftConfig) error {
	switch cfg.Policy {
	case "remediate", "alert_only", "remediate_and_alert":
	default:
		return fmt.Errorf("invalid drift.policy %q", cfg.Policy)
	}

	if (cfg.Policy == "alert_only" || cfg.Policy == "remediate_and_alert") && cfg.AlertWebhook == "" {
		return fmt.Errorf("drift.alert_webhook is required when drift.policy is %q", cfg.Policy)
	}

	return nil
}

func validateServerTLSConfig(cfg ServerTLSConfig) error {
	if cfg.Enabled && (cfg.CertFile == "" || cfg.KeyFile == "") {
		return fmt.Errorf("tls.cert_file and tls.key_file are required when tls.enabled is true")
	}
	return nil
}

func validateEventStoreConfig(cfg EventStoreConfig) error {
	switch cfg.Backend {
	case "memory", "redis":
	default:
		return fmt.Errorf("invalid event_store.backend %q", cfg.Backend)
	}

	if cfg.Backend == "redis" && cfg.Redis.URL == "" {
		return fmt.Errorf("event_store.redis.url is required when backend is redis")
	}

	return nil
}

func validateLogConfig(cfg LogConfig) error {
	switch cfg.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid log.level %q", cfg.Level)
	}

	switch cfg.Format {
	case "text", "json":
	default:
		return fmt.Errorf("invalid log.format %q", cfg.Format)
	}

	return nil
}
