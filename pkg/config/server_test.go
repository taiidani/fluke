package config

import (
	"os"
	"testing"
	"time"
)

func writeServerHCL(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "server*.hcl")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

func TestLoadServer_Minimal(t *testing.T) {
	path := writeServerHCL(t, `
server {
  agent_tokens = ["tok1"]
}
`)
	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if cfg.ListenGRPC != ":7070" {
		t.Errorf("ListenGRPC = %q, want :7070", cfg.ListenGRPC)
	}
	if cfg.ListenHTTP != ":7071" {
		t.Errorf("ListenHTTP = %q, want :7071", cfg.ListenHTTP)
	}
	if len(cfg.AgentTokens) != 1 || cfg.AgentTokens[0] != "tok1" {
		t.Errorf("AgentTokens = %v, want [tok1]", cfg.AgentTokens)
	}
	if cfg.Drift.Policy != "remediate" {
		t.Errorf("Drift.Policy = %q, want remediate", cfg.Drift.Policy)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want info", cfg.Log.Level)
	}
	if !cfg.TLS.Enabled {
		t.Error("TLS.Enabled = false, want true by documented default")
	}
	if cfg.TLS.TLSEnabled() {
		t.Error("TLSEnabled = true, want false without cert/key")
	}
	if cfg.EventStore.Backend != "memory" {
		t.Errorf("EventStore.Backend = %q, want memory", cfg.EventStore.Backend)
	}
	if cfg.EventStore.Memory.MaxEventsPerAgent != 100 {
		t.Errorf("EventStore.Memory.MaxEventsPerAgent = %d, want 100", cfg.EventStore.Memory.MaxEventsPerAgent)
	}
}

func TestLoadServer_Full(t *testing.T) {
	path := writeServerHCL(t, `
server {
  listen_grpc    = ":9090"
  listen_http    = ":9091"
  agent_tokens   = ["tok-a", "tok-b"]

  drift {
    policy        = "remediate_and_alert"
    alert_webhook = "https://hooks.example.com/abc"
  }

  log {
    level  = "debug"
    format = "json"
  }
}
`)
	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if cfg.ListenGRPC != ":9090" {
		t.Errorf("ListenGRPC = %q, want :9090", cfg.ListenGRPC)
	}
	if len(cfg.AgentTokens) != 2 {
		t.Errorf("AgentTokens = %v, want 2 tokens", cfg.AgentTokens)
	}
	if cfg.Drift.Policy != "remediate_and_alert" {
		t.Errorf("Drift.Policy = %q, want remediate_and_alert", cfg.Drift.Policy)
	}
	if cfg.Drift.AlertWebhook != "https://hooks.example.com/abc" {
		t.Errorf("Drift.AlertWebhook = %q", cfg.Drift.AlertWebhook)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want debug", cfg.Log.Level)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("Log.Format = %q, want json", cfg.Log.Format)
	}
}

func TestLoadServer_GitBlock(t *testing.T) {
	path := writeServerHCL(t, `
server {
  agent_tokens = ["tok1"]

  git {
    url           = "https://github.com/example/repo"
    branch        = "main"
    poll_interval = "30s"
    manifest_glob = "infra/**/*.fluke.hcl"
  }
}
`)
	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if cfg.Git.URL != "https://github.com/example/repo" {
		t.Errorf("Git.URL = %q", cfg.Git.URL)
	}
	if cfg.Git.Branch != "main" {
		t.Errorf("Git.Branch = %q, want main", cfg.Git.Branch)
	}
	if cfg.Git.PollInterval != 30*time.Second {
		t.Errorf("Git.PollInterval = %v, want 30s", cfg.Git.PollInterval)
	}
	if cfg.Git.ManifestGlob != "infra/**/*.fluke.hcl" {
		t.Errorf("Git.ManifestGlob = %q", cfg.Git.ManifestGlob)
	}
}

func TestLoadServer_GitBlock_Defaults(t *testing.T) {
	path := writeServerHCL(t, `
server {
  agent_tokens = ["tok1"]
  git {
    url = "https://github.com/example/repo"
  }
}
`)
	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if cfg.Git.Branch != "main" {
		t.Errorf("Git.Branch default = %q, want main", cfg.Git.Branch)
	}
	if cfg.Git.PollInterval != 60*time.Second {
		t.Errorf("Git.PollInterval default = %v, want 60s", cfg.Git.PollInterval)
	}
	if cfg.Git.ManifestGlob != "**/*.fluke.hcl" {
		t.Errorf("Git.ManifestGlob default = %q, want **/*.fluke.hcl", cfg.Git.ManifestGlob)
	}
}

func TestLoadServer_GitBlock_RequiresURL(t *testing.T) {
	path := writeServerHCL(t, `
server {
  agent_tokens = ["tok1"]
  git {}
}
`)
	_, err := LoadServer(path)
	if err == nil {
		t.Fatal("expected error for git block without url")
	}
}

func TestLoadServer_TLSBlock(t *testing.T) {
	path := writeServerHCL(t, `
server {
  agent_tokens = ["tok1"]
  tls {
    cert_file = "/etc/certs/server.crt"
    key_file  = "/etc/certs/server.key"
  }
}
`)
	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if !cfg.TLS.TLSEnabled() {
		t.Error("TLSEnabled = false, want true when both cert and key are set")
	}
	if !cfg.TLS.Enabled {
		t.Error("TLS.Enabled = false, want true")
	}
	if cfg.TLS.CertFile != "/etc/certs/server.crt" {
		t.Errorf("CertFile = %q", cfg.TLS.CertFile)
	}
}

func TestLoadServer_TLSDisabledAllowsMissingCertKey(t *testing.T) {
	path := writeServerHCL(t, `
server {
  agent_tokens = ["tok1"]
  tls {
    enabled = false
  }
}
`)
	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if cfg.TLS.Enabled {
		t.Error("TLS.Enabled = true, want false")
	}
	if cfg.TLS.TLSEnabled() {
		t.Error("TLSEnabled = true, want false")
	}
}

func TestLoadServer_TLSEnabledRequiresCertAndKey(t *testing.T) {
	path := writeServerHCL(t, `
server {
  agent_tokens = ["tok1"]
  tls {
    enabled = true
    cert_file = "/etc/certs/server.crt"
  }
}
`)
	_, err := LoadServer(path)
	if err == nil {
		t.Fatal("expected error when tls.enabled=true and key_file missing")
	}
}

func TestLoadServer_EventStoreRedis(t *testing.T) {
	path := writeServerHCL(t, `
server {
  agent_tokens = ["tok1"]
  event_store {
    backend = "redis"
    redis {
      url = "redis://localhost:6379/0"
    }
  }
}
`)
	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if cfg.EventStore.Backend != "redis" {
		t.Errorf("EventStore.Backend = %q, want redis", cfg.EventStore.Backend)
	}
	if cfg.EventStore.Redis.URL != "redis://localhost:6379/0" {
		t.Errorf("EventStore.Redis.URL = %q", cfg.EventStore.Redis.URL)
	}
	if cfg.EventStore.Redis.Prefix != "fluke" {
		t.Errorf("EventStore.Redis.Prefix = %q, want fluke", cfg.EventStore.Redis.Prefix)
	}
	if cfg.EventStore.Redis.TTL != 168*time.Hour {
		t.Errorf("EventStore.Redis.TTL = %v, want 168h", cfg.EventStore.Redis.TTL)
	}
}

func TestLoadServer_EventStoreRedisRequiresURL(t *testing.T) {
	path := writeServerHCL(t, `
server {
  agent_tokens = ["tok1"]
  event_store {
    backend = "redis"
    redis {}
  }
}
`)
	_, err := LoadServer(path)
	if err == nil {
		t.Fatal("expected error for redis backend without redis.url")
	}
}

func TestLoadServer_InvalidEventStoreBackend(t *testing.T) {
	path := writeServerHCL(t, `
server {
  agent_tokens = ["tok1"]
  event_store {
    backend = "sqlite"
  }
}
`)
	_, err := LoadServer(path)
	if err == nil {
		t.Fatal("expected error for invalid event_store backend")
	}
}

func TestServerTLSConfig_TLSEnabled(t *testing.T) {
	cases := []struct {
		cert, key string
		want      bool
	}{
		{"cert.pem", "key.pem", true},
		{"cert.pem", "", false},
		{"", "key.pem", false},
		{"", "", false},
	}
	for _, tc := range cases {
		c := ServerTLSConfig{Enabled: true, CertFile: tc.cert, KeyFile: tc.key}
		if got := c.TLSEnabled(); got != tc.want {
			t.Errorf("TLSEnabled(%q, %q) = %v, want %v", tc.cert, tc.key, got, tc.want)
		}
	}
}

func TestLoadServer_UndocumentedCheckIntervalRejected(t *testing.T) {
	path := writeServerHCL(t, `
server {
  agent_tokens   = ["tok1"]
  check_interval = "1m"
}
`)
	_, err := LoadServer(path)
	if err == nil {
		t.Error("expected error for undocumented check_interval")
	}
}

func TestLoadServer_InvalidDriftPolicy(t *testing.T) {
	path := writeServerHCL(t, `
server {
  agent_tokens = ["tok1"]
  drift {
    policy = "invalid"
  }
}
`)
	_, err := LoadServer(path)
	if err == nil {
		t.Fatal("expected error for invalid drift policy")
	}
}

func TestLoadServer_DriftPolicyRequiresAlertWebhook(t *testing.T) {
	cases := []struct {
		name   string
		policy string
	}{
		{name: "alert_only", policy: "alert_only"},
		{name: "remediate_and_alert", policy: "remediate_and_alert"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeServerHCL(t, `
server {
  agent_tokens = ["tok1"]
  drift {
    policy = "`+tc.policy+`"
  }
}
`)
			_, err := LoadServer(path)
			if err == nil {
				t.Fatalf("expected error for policy %q without alert_webhook", tc.policy)
			}
		})
	}
}

func TestLoadServer_InvalidLogLevel(t *testing.T) {
	path := writeServerHCL(t, `
server {
  agent_tokens = ["tok1"]
  log {
    level = "verbose"
  }
}
`)
	_, err := LoadServer(path)
	if err == nil {
		t.Fatal("expected error for invalid log level")
	}
}

func TestLoadServer_InvalidLogFormat(t *testing.T) {
	path := writeServerHCL(t, `
server {
  agent_tokens = ["tok1"]
  log {
    format = "yaml"
  }
}
`)
	_, err := LoadServer(path)
	if err == nil {
		t.Fatal("expected error for invalid log format")
	}
}

func TestLoadServer_AgentTokensMustNotBeEmpty(t *testing.T) {
	os.Unsetenv("FLUKE_AGENT_TOKENS")

	path := writeServerHCL(t, `
server {
  agent_tokens = []
}
`)
	_, err := LoadServer(path)
	if err == nil {
		t.Fatal("expected error for empty agent_tokens")
	}
}

func TestLoadServer_AgentTokensFallBackToEnvWhenHCLListEmptyAfterTrim(t *testing.T) {
	t.Setenv("FLUKE_AGENT_TOKENS", "env-a, env-b ,, env-c")

	path := writeServerHCL(t, `
server {
  agent_tokens = ["", "   "]
}
`)
	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if len(cfg.AgentTokens) != 3 {
		t.Fatalf("AgentTokens len = %d, want 3 (%v)", len(cfg.AgentTokens), cfg.AgentTokens)
	}
	if cfg.AgentTokens[0] != "env-a" || cfg.AgentTokens[1] != "env-b" || cfg.AgentTokens[2] != "env-c" {
		t.Errorf("AgentTokens = %v, want [env-a env-b env-c]", cfg.AgentTokens)
	}
}

func TestLoadServer_AgentTokensFallBackToEnvWhenOmitted(t *testing.T) {
	t.Setenv("FLUKE_AGENT_TOKENS", "env-a, env-b ,, env-c")

	path := writeServerHCL(t, `
server {
}
`)
	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if len(cfg.AgentTokens) != 3 {
		t.Fatalf("AgentTokens len = %d, want 3 (%v)", len(cfg.AgentTokens), cfg.AgentTokens)
	}
	if cfg.AgentTokens[0] != "env-a" || cfg.AgentTokens[1] != "env-b" || cfg.AgentTokens[2] != "env-c" {
		t.Errorf("AgentTokens = %v, want [env-a env-b env-c]", cfg.AgentTokens)
	}
}

func TestLoadServer_AgentTokensHCLTakesPrecedenceOverEnv(t *testing.T) {
	t.Setenv("FLUKE_AGENT_TOKENS", "env-a,env-b")

	path := writeServerHCL(t, `
server {
  agent_tokens = [" hcl-a ", "hcl-b"]
}
`)
	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if len(cfg.AgentTokens) != 2 {
		t.Fatalf("AgentTokens len = %d, want 2 (%v)", len(cfg.AgentTokens), cfg.AgentTokens)
	}
	if cfg.AgentTokens[0] != "hcl-a" || cfg.AgentTokens[1] != "hcl-b" {
		t.Errorf("AgentTokens = %v, want [hcl-a hcl-b]", cfg.AgentTokens)
	}
}

func TestLoadServer_NonTokenFieldsTreatDollarLiterally(t *testing.T) {
	os.Setenv("TEST_LISTEN_GRPC", ":8181")
	defer os.Unsetenv("TEST_LISTEN_GRPC")
	os.Setenv("TEST_LISTEN_HTTP", ":8282")
	defer os.Unsetenv("TEST_LISTEN_HTTP")

	path := writeServerHCL(t, `
server {
  listen_grpc  = "$TEST_LISTEN_GRPC"
  listen_http  = "$${TEST_LISTEN_HTTP}"
  agent_tokens = ["tok1"]
}
`)
	cfg, err := LoadServer(path)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if cfg.ListenGRPC != "$TEST_LISTEN_GRPC" {
		t.Errorf("ListenGRPC = %q, want $TEST_LISTEN_GRPC", cfg.ListenGRPC)
	}
	if cfg.ListenHTTP != "${TEST_LISTEN_HTTP}" {
		t.Errorf("ListenHTTP = %q, want ${TEST_LISTEN_HTTP}", cfg.ListenHTTP)
	}
}

func TestLoadServer_NonexistentFile(t *testing.T) {
	_, err := LoadServer("/nonexistent/path/server.hcl")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}
