package codexupgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

func LookupLatestVersion(ctx context.Context, opts LatestVersionOptions) (string, error) {
	packageName := firstNonEmpty(strings.TrimSpace(opts.PackageName), defaultPackageName)
	registryURL := firstNonEmpty(strings.TrimSpace(opts.RegistryURL), "https://registry.npmjs.org")
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	rawURL := strings.TrimRight(registryURL, "/") + "/" + url.PathEscape(packageName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("registry lookup failed: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		DistTags map[string]string `json:"dist-tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	latest := strings.TrimSpace(payload.DistTags["latest"])
	if latest == "" {
		return "", fmt.Errorf("registry response missing latest dist-tag for %s", packageName)
	}
	return latest, nil
}

func InstallGlobal(ctx context.Context, version string, opts InstallOptions) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return fmt.Errorf("missing target version")
	}
	npmCommand := firstNonEmpty(strings.TrimSpace(opts.NPMCommand), defaultNPMCommand)
	packageName := firstNonEmpty(strings.TrimSpace(opts.PackageName), defaultPackageName)
	packageSpec := packageName + "@" + version
	if _, err := runCommand(ctx, npmCommand, "install", "-g", packageSpec); err != nil {
		return fmt.Errorf("npm install %s failed: %w", packageSpec, err)
	}
	return nil
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", exec.ErrNotFound
	}
	cmd := execlaunch.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return string(output), nil
	}
	text := cleanCommandOutput(string(output))
	if text == "" {
		return "", err
	}
	return "", fmt.Errorf("%w: %s", err, text)
}

func cleanCommandOutput(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
}
