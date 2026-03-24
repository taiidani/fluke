package config

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	t.Run("empty uses default", func(t *testing.T) {
		d, err := parseDuration("", 5*time.Minute)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d != 5*time.Minute {
			t.Errorf("got %v, want 5m", d)
		}
	})

	t.Run("valid duration", func(t *testing.T) {
		d, err := parseDuration("30s", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d != 30*time.Second {
			t.Errorf("got %v, want 30s", d)
		}
	})

	t.Run("invalid duration", func(t *testing.T) {
		_, err := parseDuration("not-a-duration", 0)
		if err == nil {
			t.Error("expected error for invalid duration")
		}
	})
}

func TestCoalesce(t *testing.T) {
	if got := coalesce("hello", "world"); got != "hello" {
		t.Errorf("coalesce(%q, %q) = %q, want %q", "hello", "world", got, "hello")
	}
	if got := coalesce("", "world"); got != "world" {
		t.Errorf("coalesce(%q, %q) = %q, want %q", "", "world", got, "world")
	}
}

func TestTrimmedNonEmptyStrings(t *testing.T) {
	input := []string{"  a  ", "", "   ", "b"}
	got := trimmedNonEmptyStrings(input)

	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (%v)", len(got), got)
	}
	if got[0] != "a" || got[1] != "b" {
		t.Errorf("got = %v, want [a b]", got)
	}
}

func TestTrimmedCSVNonEmpty(t *testing.T) {
	got := trimmedCSVNonEmpty(" a, b ,,   ,c ")

	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3 (%v)", len(got), got)
	}
	if got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("got = %v, want [a b c]", got)
	}
}

func TestNewLogger_DoesNotPanic(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error", "unknown"} {
		for _, format := range []string{"text", "json"} {
			l := NewLogger(LogConfig{Level: level, Format: format})
			if l == nil {
				t.Errorf("NewLogger(%q, %q) returned nil", level, format)
			}
		}
	}
}
