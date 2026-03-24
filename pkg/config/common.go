package config

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

// LogConfig controls structured log output. Shared between server and agent.
type LogConfig struct {
	Level  string // debug | info | warn | error (default: info)
	Format string // text | json (default: text)
}

// NewLogger builds an slog.Logger from a LogConfig.
func NewLogger(cfg LogConfig) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

func trimmedNonEmptyStrings(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		t := strings.TrimSpace(s)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func trimmedCSVNonEmpty(s string) []string {
	return trimmedNonEmptyStrings(strings.Split(s, ","))
}

// parseDuration parses a Go duration string, returning def if s is empty.
func parseDuration(s string, def time.Duration) (time.Duration, error) {
	if s == "" {
		return def, nil
	}
	return time.ParseDuration(s)
}

// coalesce returns s if non-empty, otherwise def.
func coalesce(s, def string) string {
	if s != "" {
		return s
	}
	return def
}
