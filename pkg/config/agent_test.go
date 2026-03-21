package config

import (
	"os"
	"testing"
	"time"
)

func writeAgentHCL(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "agent*.hcl")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

func TestLoadAgent_Minimal(t *testing.T) {
	path := writeAgentHCL(t, `
agent {
  server_url = "grpc://localhost:7070"
  token      = "dev-token"
}
`)
	cfg, err := LoadAgent(path)
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}
	if cfg.ServerURL != "grpc://localhost:7070" {
		t.Errorf("ServerURL = %q", cfg.ServerURL)
	}
	if cfg.Token != "dev-token" {
		t.Errorf("Token = %q", cfg.Token)
	}
	if cfg.Labels == nil {
		t.Error("Labels should not be nil")
	}
	if cfg.Execution.DefaultShell != "/bin/bash" {
		t.Errorf("Execution.DefaultShell = %q, want /bin/bash", cfg.Execution.DefaultShell)
	}
	if cfg.Execution.CommandTimeout != 5*time.Minute {
		t.Errorf("Execution.CommandTimeout = %v, want 5m", cfg.Execution.CommandTimeout)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want info", cfg.Log.Level)
	}
}

func TestLoadAgent_Full(t *testing.T) {
	path := writeAgentHCL(t, `
agent {
  server_url = "grpcs://server.example.com:7070"
  token      = "prod-token"
  name       = "web-01"

  labels = {
    role = "web"
    env  = "prod"
  }

  execution {
    default_shell   = "/bin/sh"
    command_timeout = "10m"
  }

  log {
    level  = "warn"
    format = "json"
  }
}
`)
	cfg, err := LoadAgent(path)
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}
	if cfg.ServerURL != "grpcs://server.example.com:7070" {
		t.Errorf("ServerURL = %q", cfg.ServerURL)
	}
	if cfg.Name != "web-01" {
		t.Errorf("Name = %q, want web-01", cfg.Name)
	}
	if cfg.Labels["role"] != "web" || cfg.Labels["env"] != "prod" {
		t.Errorf("Labels = %v", cfg.Labels)
	}
	if cfg.Execution.DefaultShell != "/bin/sh" {
		t.Errorf("Execution.DefaultShell = %q, want /bin/sh", cfg.Execution.DefaultShell)
	}
	if cfg.Execution.CommandTimeout != 10*time.Minute {
		t.Errorf("Execution.CommandTimeout = %v, want 10m", cfg.Execution.CommandTimeout)
	}
	if cfg.Log.Level != "warn" {
		t.Errorf("Log.Level = %q, want warn", cfg.Log.Level)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("Log.Format = %q, want json", cfg.Log.Format)
	}
}

func TestLoadAgent_NameOmitted(t *testing.T) {
	wantHost, err := os.Hostname()
	if err != nil {
		t.Fatalf("os.Hostname: %v", err)
	}
	if wantHost == "" {
		t.Fatal("os.Hostname returned empty hostname")
	}

	path := writeAgentHCL(t, `
agent {
  server_url = "grpc://localhost:7070"
  token      = "dev-token"
}
`)
	cfg, err := LoadAgent(path)
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}
	if cfg.Name != wantHost {
		t.Errorf("Name = %q, want hostname %q when omitted", cfg.Name, wantHost)
	}
}

func TestLoadAgent_TLSBlock(t *testing.T) {
	path := writeAgentHCL(t, `
agent {
  server_url = "grpcs://server.example.com:7070"
  token      = "tok1"

  tls {
    ca_file              = "/etc/certs/ca.crt"
    insecure_skip_verify = false
  }
}
`)
	cfg, err := LoadAgent(path)
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}
	if cfg.TLS.CAFile != "/etc/certs/ca.crt" {
		t.Errorf("TLS.CAFile = %q", cfg.TLS.CAFile)
	}
	if cfg.TLS.InsecureSkipVerify {
		t.Error("TLS.InsecureSkipVerify = true, want false")
	}
}

func TestLoadAgent_UndocumentedMaxConcurrencyRejected(t *testing.T) {
	path := writeAgentHCL(t, `
agent {
  server_url = "grpc://localhost:7070"
  token      = "tok1"

  execution {
    max_concurrency = 2
  }
}
`)
	_, err := LoadAgent(path)
	if err == nil {
		t.Error("expected error for undocumented execution.max_concurrency")
	}
}

func TestLoadAgent_TokenFallsBackToEnvWhenHCLTokenIsBlank(t *testing.T) {
	t.Setenv("FLUKE_TOKEN", "env-token")

	path := writeAgentHCL(t, `
agent {
  server_url = "grpc://localhost:7070"
  token      = "   "
}
`)
	cfg, err := LoadAgent(path)
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}
	if cfg.Token != "env-token" {
		t.Errorf("Token = %q, want env-token", cfg.Token)
	}
}

func TestLoadAgent_TokenFallsBackToEnvWhenTokenOmitted(t *testing.T) {
	t.Setenv("FLUKE_TOKEN", "env-token")

	path := writeAgentHCL(t, `
agent {
  server_url = "grpc://localhost:7070"
}
`)
	cfg, err := LoadAgent(path)
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}
	if cfg.Token != "env-token" {
		t.Errorf("Token = %q, want env-token", cfg.Token)
	}
}

func TestLoadAgent_HCLTokenTakesPrecedenceOverEnv(t *testing.T) {
	t.Setenv("FLUKE_TOKEN", "env-token")

	path := writeAgentHCL(t, `
agent {
  server_url = "grpc://localhost:7070"
  token      = "hcl-token"
}
`)
	cfg, err := LoadAgent(path)
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}
	if cfg.Token != "hcl-token" {
		t.Errorf("Token = %q, want hcl-token", cfg.Token)
	}
}

func TestLoadAgent_NonTokenFieldsTreatDollarLiterally(t *testing.T) {
	os.Setenv("TEST_AGENT_ROLE", "web")
	defer os.Unsetenv("TEST_AGENT_ROLE")
	os.Setenv("TEST_AGENT_NAME", "agent-from-env")
	defer os.Unsetenv("TEST_AGENT_NAME")

	path := writeAgentHCL(t, `
agent {
  server_url = "grpc://localhost:7070"
  token      = "tok1"
  name       = "$${TEST_AGENT_NAME}"
  labels = {
    role = "$TEST_AGENT_ROLE"
  }
}
`)
	cfg, err := LoadAgent(path)
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}
	if cfg.Labels["role"] != "$TEST_AGENT_ROLE" {
		t.Errorf("Labels[role] = %q, want $TEST_AGENT_ROLE", cfg.Labels["role"])
	}
	if cfg.Name != "${TEST_AGENT_NAME}" {
		t.Errorf("Name = %q, want ${TEST_AGENT_NAME}", cfg.Name)
	}
}

func TestLoadAgent_InvalidCommandTimeout(t *testing.T) {
	path := writeAgentHCL(t, `
agent {
  server_url = "grpc://localhost:7070"
  token      = "tok1"

  execution {
    command_timeout = "bad-duration"
  }
}
`)
	_, err := LoadAgent(path)
	if err == nil {
		t.Error("expected error for invalid command_timeout")
	}
}

func TestLoadAgent_InvalidServerURLScheme(t *testing.T) {
	path := writeAgentHCL(t, `
agent {
  server_url = "http://localhost:7070"
  token      = "tok1"
}
`)
	_, err := LoadAgent(path)
	if err == nil {
		t.Fatal("expected error for invalid server_url scheme")
	}
}

func TestLoadAgent_EmptyToken(t *testing.T) {
	os.Unsetenv("FLUKE_TOKEN")

	path := writeAgentHCL(t, `
agent {
  server_url = "grpc://localhost:7070"
  token      = ""
}
`)
	_, err := LoadAgent(path)
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestLoadAgent_InvalidLogLevel(t *testing.T) {
	path := writeAgentHCL(t, `
agent {
  server_url = "grpc://localhost:7070"
  token      = "tok1"
  log {
    level = "trace"
  }
}
`)
	_, err := LoadAgent(path)
	if err == nil {
		t.Fatal("expected error for invalid log level")
	}
}

func TestLoadAgent_InvalidLogFormat(t *testing.T) {
	path := writeAgentHCL(t, `
agent {
  server_url = "grpc://localhost:7070"
  token      = "tok1"
  log {
    format = "yaml"
  }
}
`)
	_, err := LoadAgent(path)
	if err == nil {
		t.Fatal("expected error for invalid log format")
	}
}

func TestLoadAgent_NonexistentFile(t *testing.T) {
	_, err := LoadAgent("/nonexistent/path/agent.hcl")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadAgent_DevConfig(t *testing.T) {
	// Verify the dev/agent.hcl shipped with the repo parses cleanly.
	cfg, err := LoadAgent("../../dev/agent.hcl")
	if err != nil {
		t.Fatalf("LoadAgent(dev/agent.hcl): %v", err)
	}
	if cfg.ServerURL != "grpc://localhost:7070" {
		t.Errorf("ServerURL = %q", cfg.ServerURL)
	}
	if cfg.Token != "dev-token" {
		t.Errorf("Token = %q", cfg.Token)
	}
}
