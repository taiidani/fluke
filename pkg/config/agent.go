package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2/hclsimple"
)

// ── Raw HCL structs ───────────────────────────────────────────────────────────

type rawAgentFile struct {
	Agent rawAgentBlock `hcl:"agent,block"`
}

type rawAgentBlock struct {
	ServerURL string            `hcl:"server_url,attr"`
	Token     string            `hcl:"token,optional"`
	Name      string            `hcl:"name,optional"`
	Labels    map[string]string `hcl:"labels,optional"`

	TLS       []rawAgentTLS       `hcl:"tls,block"`
	Execution []rawExecutionBlock `hcl:"execution,block"`
	Log       []rawLogBlock       `hcl:"log,block"`
}

type rawAgentTLS struct {
	CAFile             string `hcl:"ca_file,optional"`
	InsecureSkipVerify bool   `hcl:"insecure_skip_verify,optional"`
}

type rawExecutionBlock struct {
	DefaultShell   string `hcl:"default_shell,optional"`
	CommandTimeout string `hcl:"command_timeout,optional"`
}

// ── Resolved config types ─────────────────────────────────────────────────────

// AgentConfig is the fully parsed and defaulted agent configuration.
type AgentConfig struct {
	ServerURL string
	Token     string
	Name      string
	Labels    map[string]string
	TLS       AgentTLSConfig
	Execution ExecutionConfig
	Log       LogConfig
}

// AgentTLSConfig holds TLS verification settings for the agent's outbound
// connection to the server.
type AgentTLSConfig struct {
	// CAFile is the path to a CA certificate used to verify the server's cert.
	// Required when the server uses a self-signed certificate.
	CAFile string
	// InsecureSkipVerify disables server certificate verification.
	// Development only — never use in production.
	InsecureSkipVerify bool
}

// ExecutionConfig holds settings that control how the agent runs commands.
type ExecutionConfig struct {
	DefaultShell   string
	CommandTimeout time.Duration
}

// ── Loader ────────────────────────────────────────────────────────────────────

// LoadAgent parses an agent HCL config file and returns a fully resolved
// AgentConfig with defaults applied and token env fallback resolution.
func LoadAgent(path string) (*AgentConfig, error) {
	var raw rawAgentFile
	if err := hclsimple.DecodeFile(path, nil, &raw); err != nil {
		return nil, fmt.Errorf("parse agent config %q: %w", path, err)
	}
	return resolveAgent(raw.Agent)
}

func resolveAgent(r rawAgentBlock) (*AgentConfig, error) {
	token := strings.TrimSpace(r.Token)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("FLUKE_TOKEN"))
	}

	cfg := &AgentConfig{
		ServerURL: r.ServerURL,
		Token:     token,
		Name:      strings.TrimSpace(r.Name),
		Labels:    r.Labels,
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("token is required")
	}
	if cfg.Name == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("resolve default agent name from hostname: %w", err)
		}
		cfg.Name = hostname
	}
	if !strings.HasPrefix(cfg.ServerURL, "grpc://") && !strings.HasPrefix(cfg.ServerURL, "grpcs://") {
		return nil, fmt.Errorf("invalid server_url %q: must use grpc:// or grpcs://", cfg.ServerURL)
	}
	if cfg.Labels == nil {
		cfg.Labels = make(map[string]string)
	}

	if len(r.TLS) > 0 {
		t := r.TLS[0]
		cfg.TLS = AgentTLSConfig{
			CAFile:             t.CAFile,
			InsecureSkipVerify: t.InsecureSkipVerify,
		}
	}

	cfg.Execution = ExecutionConfig{
		DefaultShell:   "/bin/bash",
		CommandTimeout: 5 * time.Minute,
	}
	if len(r.Execution) > 0 {
		e := r.Execution[0]
		timeout, err := parseDuration(e.CommandTimeout, 5*time.Minute)
		if err != nil {
			return nil, fmt.Errorf("invalid execution.command_timeout %q: %w", e.CommandTimeout, err)
		}
		cfg.Execution = ExecutionConfig{
			DefaultShell:   coalesce(e.DefaultShell, "/bin/bash"),
			CommandTimeout: timeout,
		}
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
