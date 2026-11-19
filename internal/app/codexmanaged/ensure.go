// Package codexmanaged keeps daemon-owned Codex children on a private stable
// installation without changing the user's CLI or shared app-server daemon.
package codexmanaged

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

const checkInterval = 15 * time.Minute

type operations struct {
	lookup      func(context.Context) (string, error)
	install     func(context.Context, string, string) error
	readVersion func(context.Context, string) (string, error)
	now         func() time.Time
}

type checkRecord struct {
	Latest    string    `json:"latest"`
	CheckedAt time.Time `json:"checkedAt"`
}

type candidate struct {
	binary  string
	version version
}

// Ensure selects a verified Codex binary, installing newer stable versions into
// root when needed. A confirmed update that cannot be installed is an error;
// an unavailable registry permits a verified existing binary to run offline.
// It never changes CODEX_HOME, credentials, global npm installs or daemon state.
func Ensure(ctx context.Context, configuredBinary, root string) (string, error) {
	return EnsureWithEnv(ctx, configuredBinary, root, os.Environ())
}

// EnsureWithEnv uses only the supplied child environment for process launch and
// registry proxy selection. It does not mutate the calling process environment.
func EnsureWithEnv(ctx context.Context, configuredBinary, root string, env []string) (string, error) {
	env = append([]string{}, env...)
	resolved, err := configuredCodexBinary(configuredBinary, env)
	if err != nil {
		return "", fmt.Errorf("resolve configured Codex before private update: %w", err)
	}
	configuredBinary = resolved
	return ensure(ctx, configuredBinary, root, operations{
		lookup: func(ctx context.Context) (string, error) { return lookupLatestWithEnv(ctx, env) },
		install: func(ctx context.Context, staging, target string) error {
			return installPrivateWithEnv(ctx, staging, target, env)
		},
		readVersion: func(ctx context.Context, binary string) (string, error) {
			return readBinaryVersionWithEnv(ctx, binary, env)
		},
		now: time.Now,
	})
}

func ensure(ctx context.Context, configuredBinary, root string, ops operations) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("private Codex install root is empty")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve private Codex install root: %w", err)
	}
	lock, err := relayruntime.AcquireLock(ctx, filepath.Join(root, "update.lock"), true)
	if err != nil {
		return "", fmt.Errorf("lock private Codex update: %w", err)
	}
	defer lock.Release()

	configured, configuredErr := verifyCandidate(ctx, configuredBinary, ops)
	if configuredErr != nil {
		return "", fmt.Errorf("verify configured Codex before private update: %w", configuredErr)
	}
	best, err := newestPrivate(ctx, root, ops)
	if err != nil {
		return "", err
	}
	if best.binary == "" || configured.version.newerThan(best.version) {
		best = configured
	}
	// A configured preview is an explicit choice, not a request for the stable channel.
	if configured.version.preview != "" {
		return best.binary, nil
	}
	latest, err := latestVersion(ctx, root, ops)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if best.binary != "" {
			log.Printf("codex managed: stable version check unavailable; using verified Codex %s: %v", best.version.text, err)
			return best.binary, nil
		}
		return "", fmt.Errorf("Codex version check failed and no verified binary is available: %w", err)
	}
	if best.binary != "" && !latest.newerThan(best.version) {
		return best.binary, nil
	}
	release := filepath.Join(root, "releases", latest.text)
	if _, err := os.Stat(release); err == nil {
		// Preserve any in-use executable inode while removing a corrupt release
		// from selection. Quarantines live outside releases and are never selected.
		quarantine, err := os.MkdirTemp(root, ".quarantine-"+latest.text+"-")
		if err != nil {
			return "", err
		}
		if err := os.Remove(quarantine); err != nil {
			return "", err
		}
		if err := os.Rename(release, quarantine); err != nil {
			return "", fmt.Errorf("quarantine invalid private Codex %s: %w", latest.text, err)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(release), 0o700); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(root, ".install-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	if err := ops.install(ctx, staging, latest.text); err != nil {
		return "", fmt.Errorf("install private Codex %s: %w", latest.text, err)
	}
	binary, err := installedBinary(staging)
	if err != nil {
		return "", err
	}
	installed, err := verifyCandidate(ctx, binary, ops)
	if err != nil {
		return "", fmt.Errorf("verify private Codex %s: %w", latest.text, err)
	}
	if installed.version.text != latest.text {
		return "", fmt.Errorf("private Codex install returned version %s, expected %s", installed.version.text, latest.text)
	}
	relative, err := filepath.Rel(staging, binary)
	if err != nil {
		return "", err
	}
	if err := os.Rename(staging, release); err != nil {
		return "", fmt.Errorf("publish private Codex %s: %w", latest.text, err)
	}
	return filepath.Join(release, relative), nil
}

func verifyCandidate(ctx context.Context, binary string, ops operations) (candidate, error) {
	if strings.TrimSpace(binary) == "" {
		return candidate{}, fmt.Errorf("configured Codex binary is empty")
	}
	text, err := ops.readVersion(ctx, binary)
	if err != nil {
		return candidate{}, err
	}
	parsed, err := parseVersion(text)
	return candidate{binary: binary, version: parsed}, err
}

func newestPrivate(ctx context.Context, root string, ops operations) (candidate, error) {
	entries, err := os.ReadDir(filepath.Join(root, "releases"))
	if os.IsNotExist(err) {
		return candidate{}, nil
	}
	if err != nil {
		return candidate{}, fmt.Errorf("read private Codex releases: %w", err)
	}
	var versions []version
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		parsed, err := parseVersion(entry.Name())
		if err == nil {
			versions = append(versions, parsed)
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].newerThan(versions[j]) })
	for _, parsed := range versions {
		binary, err := installedBinary(filepath.Join(root, "releases", parsed.text))
		if err != nil {
			continue
		}
		verified, err := verifyCandidate(ctx, binary, ops)
		if err == nil && verified.version.text == parsed.text {
			return verified, nil
		}
	}
	return candidate{}, nil
}

func latestVersion(ctx context.Context, root string, ops operations) (version, error) {
	cachePath := filepath.Join(root, "check.json")
	var cached checkRecord
	if raw, err := os.ReadFile(cachePath); err == nil && json.Unmarshal(raw, &cached) == nil {
		age := ops.now().Sub(cached.CheckedAt)
		if age >= 0 && age < checkInterval {
			if parsed, err := parseVersion(cached.Latest); err == nil && parsed.preview == "" {
				return parsed, nil
			}
		}
	}
	text, err := ops.lookup(ctx)
	if err != nil {
		return version{}, err
	}
	parsed, err := parseVersion(text)
	if err != nil {
		return version{}, err
	}
	if parsed.preview != "" {
		return version{}, fmt.Errorf("stable Codex channel returned preview version %q", text)
	}
	if err := writeCheckRecord(root, cachePath, checkRecord{Latest: text, CheckedAt: ops.now().UTC()}); err != nil {
		// A cache failure must not disguise an already-confirmed update as an
		// offline check and allow an old version to start.
		log.Printf("codex managed: cannot cache successful version check: %v", err)
	}
	return parsed, nil
}

func writeCheckRecord(root, cachePath string, record checkRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(root, ".check-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, cachePath); err != nil {
		return err
	}
	return nil
}
