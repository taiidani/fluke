package manifest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"time"
)

const defaultPollInterval = 60 * time.Second

type DiscoverFunc func(root, pattern string) ([]string, error)

type OnChangeFunc func(paths []string) error

// Poller periodically discovers manifest files and invokes OnChange when the
// manifest set fingerprint changes.
type Poller struct {
	RootDir  string
	Pattern  string
	Interval time.Duration
	Discover DiscoverFunc
	OnChange OnChangeFunc
}

func (p *Poller) Run(ctx context.Context) error {
	if p.Discover == nil {
		return fmt.Errorf("manifest poller discover function is required")
	}
	if p.OnChange == nil {
		return fmt.Errorf("manifest poller on-change function is required")
	}

	interval := p.Interval
	if interval <= 0 {
		interval = defaultPollInterval
	}

	var previousFingerprint string
	hasPrevious := false

	poll := func() error {
		paths, err := p.Discover(p.RootDir, p.Pattern)
		if err != nil {
			return err
		}

		fingerprint, err := manifestSetFingerprint(paths)
		if err != nil {
			return err
		}
		if !hasPrevious {
			hasPrevious = true
			previousFingerprint = fingerprint
			return nil
		}

		if fingerprint == previousFingerprint {
			return nil
		}

		if err := p.OnChange(append([]string(nil), paths...)); err != nil {
			return nil
		}

		previousFingerprint = fingerprint
		return nil
	}

	if err := poll(); err != nil {
		return fmt.Errorf("initial manifest poll: %w", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := poll(); err != nil {
				return fmt.Errorf("manifest poll: %w", err)
			}
		}
	}
}

func manifestSetFingerprint(paths []string) (string, error) {
	normalized := append([]string(nil), paths...)
	sort.Strings(normalized)

	hasher := sha256.New()
	for _, path := range normalized {
		content, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read manifest %q: %w", path, err)
		}
		contentHash := sha256.Sum256(content)
		hasher.Write([]byte(path))
		hasher.Write([]byte("\x00"))
		hasher.Write([]byte(hex.EncodeToString(contentHash[:])))
		hasher.Write([]byte("\n"))
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
