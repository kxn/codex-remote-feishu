package daemon

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

var errBrowserUnavailable = errors.New("browser opener unavailable")

var browserOpener = defaultBrowserOpener

type startupAccessPlan struct {
	SetupRequired      bool
	SSHSession         bool
	AutoOpenBrowser    bool
	RelayBindHost      string
	RelayPort          string
	AdminBindHost      string
	AdminPort          string
	AdminURL           string
	SetupURL           string
	SetupToken         string
	SetupTokenExpiry   time.Time
	ConfiguredAppCount int
}

func buildStartupAccessPlan(appConfig config.AppConfig, services config.ServicesConfig, env map[string]string) startupAccessPlan {
	sshSession := isSSHSession(env)
	setupRequired := requiresSetup(appConfig, services)

	adminBindHost := strings.TrimSpace(services.RelayAPIHost)
	if setupRequired && sshSession {
		adminBindHost = "0.0.0.0"
	}
	adminHost := announcedAdminHost(adminBindHost, sshSession, env)
	adminURL := httpURL(adminHost, services.RelayAPIPort, "/admin/")
	setupURL := httpURL(adminHost, services.RelayAPIPort, "/setup")

	autoOpen := appConfig.Admin.AutoOpenBrowser == nil || *appConfig.Admin.AutoOpenBrowser
	return startupAccessPlan{
		SetupRequired:      setupRequired,
		SSHSession:         sshSession,
		AutoOpenBrowser:    autoOpen,
		RelayBindHost:      strings.TrimSpace(services.RelayHost),
		RelayPort:          strings.TrimSpace(services.RelayPort),
		AdminBindHost:      adminBindHost,
		AdminPort:          strings.TrimSpace(services.RelayAPIPort),
		AdminURL:           adminURL,
		SetupURL:           setupURL,
		ConfiguredAppCount: configuredRuntimeAppCount(appConfig, services),
	}
}

func requiresSetup(appConfig config.AppConfig, services config.ServicesConfig) bool {
	return configuredRuntimeAppCount(appConfig, services) == 0
}

func configuredRuntimeAppCount(appConfig config.AppConfig, services config.ServicesConfig) int {
	count := 0
	for _, app := range effectiveFeishuApps(appConfig, services) {
		if strings.TrimSpace(app.AppID) == "" || strings.TrimSpace(app.AppSecret) == "" {
			continue
		}
		count++
	}
	return count
}

func effectiveFeishuApps(appConfig config.AppConfig, services config.ServicesConfig) []config.FeishuAppConfig {
	apps := append([]config.FeishuAppConfig(nil), appConfig.Feishu.Apps...)
	if strings.TrimSpace(services.FeishuAppID) == "" && strings.TrimSpace(services.FeishuAppSecret) == "" {
		return apps
	}

	gatewayID := strings.TrimSpace(services.FeishuGatewayID)
	if gatewayID == "" {
		gatewayID = "legacy-default"
	}
	for i := range apps {
		currentID := strings.TrimSpace(apps[i].ID)
		if currentID == "" {
			currentID = "legacy-default"
		}
		if currentID != gatewayID {
			continue
		}
		apps[i].ID = gatewayID
		apps[i].AppID = services.FeishuAppID
		apps[i].AppSecret = services.FeishuAppSecret
		return apps
	}

	return append(apps, config.FeishuAppConfig{
		ID:        gatewayID,
		Name:      "Runtime Override",
		AppID:     services.FeishuAppID,
		AppSecret: services.FeishuAppSecret,
	})
}

func isSSHSession(env map[string]string) bool {
	return strings.TrimSpace(env["SSH_CONNECTION"]) != "" ||
		strings.TrimSpace(env["SSH_CLIENT"]) != "" ||
		strings.TrimSpace(env["SSH_TTY"]) != ""
}

func announcedAdminHost(bindHost string, sshSession bool, env map[string]string) string {
	bindHost = strings.TrimSpace(bindHost)
	if sshSession && isWildcardHost(bindHost) {
		if serverIP := sshServerIP(env); serverIP != "" {
			return serverIP
		}
		if detected := detectNonLoopbackIP(); detected != "" {
			return detected
		}
	}
	if isWildcardHost(bindHost) || isLoopbackHost(bindHost) {
		return "localhost"
	}
	return bindHost
}

func sshServerIP(env map[string]string) string {
	fields := strings.Fields(strings.TrimSpace(env["SSH_CONNECTION"]))
	if len(fields) < 3 {
		return ""
	}
	return normalizeIPAddress(fields[2])
}

func detectNonLoopbackIP() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			value := normalizeIPAddress(addr.String())
			if value == "" {
				continue
			}
			parsed, err := netip.ParseAddr(value)
			if err != nil || parsed.IsLoopback() {
				continue
			}
			return value
		}
	}
	return ""
}

func normalizeIPAddress(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "/") {
		ip, _, err := net.ParseCIDR(value)
		if err == nil && ip != nil {
			return ip.String()
		}
	}
	if parsed := net.ParseIP(strings.Trim(value, "[]")); parsed != nil {
		return parsed.String()
	}
	return ""
}

func isWildcardHost(host string) bool {
	host = strings.TrimSpace(host)
	return host == "" || host == "0.0.0.0" || host == "::" || host == "[::]"
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func httpURL(host, port, path string) string {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "80"
	}
	if path == "" {
		path = "/"
	}
	return fmt.Sprintf("http://%s%s", net.JoinHostPort(host, port), path)
}

func maybeOpenSetupBrowser(plan startupAccessPlan, env map[string]string) error {
	if !plan.SetupRequired || plan.SSHSession || !plan.AutoOpenBrowser || strings.TrimSpace(plan.SetupURL) == "" {
		return nil
	}
	return browserOpener(plan.SetupURL, env)
}

func defaultBrowserOpener(url string, env map[string]string) error {
	command := browserCommand(env)
	if len(command) == 0 {
		return errBrowserUnavailable
	}
	cmd := execlaunch.Command(command[0], append(command[1:], url)...)
	return cmd.Start()
}

func browserCommand(env map[string]string) []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"open"}
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler"}
	default:
		if strings.TrimSpace(env["DISPLAY"]) == "" && strings.TrimSpace(env["WAYLAND_DISPLAY"]) == "" {
			return nil
		}
		if path, err := exec.LookPath("xdg-open"); err == nil {
			return []string{path}
		}
		return nil
	}
}

func envMap(values []string) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		key, current, ok := strings.Cut(value, "=")
		if !ok {
			continue
		}
		result[key] = current
	}
	return result
}
