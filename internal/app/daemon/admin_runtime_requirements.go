package daemon

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper"
	"github.com/kxn/codex-remote-feishu/internal/config"
)

const (
	runtimeRequirementStatusPass = "pass"
	runtimeRequirementStatusWarn = "warn"
	runtimeRequirementStatusFail = "fail"
)

type runtimeRequirementsResponse struct {
	Ready                   bool                      `json:"ready"`
	Summary                 string                    `json:"summary"`
	CurrentBinary           string                    `json:"currentBinary,omitempty"`
	CodexRealBinary         string                    `json:"codexRealBinary,omitempty"`
	CodexRealBinarySource   string                    `json:"codexRealBinarySource,omitempty"`
	ResolvedCodexRealBinary string                    `json:"resolvedCodexRealBinary,omitempty"`
	LookupMode              string                    `json:"lookupMode,omitempty"`
	Checks                  []runtimeRequirementCheck `json:"checks"`
	Notes                   []string                  `json:"notes,omitempty"`
}

type runtimeRequirementCheck struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
}

func (a *App) handleRuntimeRequirementsDetect(w http.ResponseWriter, _ *http.Request) {
	payload, err := a.buildRuntimeRequirementsResponse()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "runtime_requirements_detect_failed",
			Message: "failed to detect runtime requirements",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (a *App) buildRuntimeRequirementsResponse() (runtimeRequirementsResponse, error) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return runtimeRequirementsResponse{}, err
	}
	currentBinary, err := a.currentBinaryPath()
	if err != nil {
		return runtimeRequirementsResponse{}, err
	}
	return buildRuntimeRequirementsResponseForLoaded(loaded, currentBinary)
}

func buildRuntimeRequirementsResponseForLoaded(loaded config.LoadedAppConfig, currentBinary string) (runtimeRequirementsResponse, error) {
	codexRealBinary, source := resolvedCodexRealBinarySetting(loaded)
	lookupMode := codexBinaryLookupMode(codexRealBinary)
	effectiveRealBinary, resolveErr := wrapper.ResolveNormalCodexBinaryPreview(codexRealBinary)
	resolvedRealBinary := ""
	if resolveErr == nil && strings.TrimSpace(effectiveRealBinary) != "" {
		resolvedRealBinary, resolveErr = resolveExecutablePath(effectiveRealBinary)
	}
	resolvedClaudeBinary := ""
	claudeResolveErr := error(nil)
	if value, err := config.ResolveClaudeBinary(os.Environ()); err == nil {
		resolvedClaudeBinary = value
	} else {
		claudeResolveErr = err
	}

	checks := make([]runtimeRequirementCheck, 0, 5)
	launcherReady := true
	codexReady := false
	claudeReady := false
	hasWarn := false

	if strings.TrimSpace(currentBinary) == "" {
		launcherReady = false
		checks = append(checks, runtimeRequirementCheck{
			ID:      "headless_launcher",
			Title:   "Headless 启动器",
			Status:  runtimeRequirementStatusFail,
			Summary: "当前服务没有可用的 codex-remote 二进制路径。",
		})
	} else if _, err := os.Stat(currentBinary); err != nil {
		launcherReady = false
		checks = append(checks, runtimeRequirementCheck{
			ID:      "headless_launcher",
			Title:   "Headless 启动器",
			Status:  runtimeRequirementStatusFail,
			Summary: "当前服务记录的 codex-remote 二进制路径不可访问。",
			Detail:  err.Error(),
		})
	} else {
		checks = append(checks, runtimeRequirementCheck{
			ID:      "headless_launcher",
			Title:   "Headless 启动器",
			Status:  runtimeRequirementStatusPass,
			Summary: "当前服务已经有可用的 codex-remote 启动器。",
			Detail:  currentBinary,
		})
	}

	if strings.TrimSpace(codexRealBinary) == "" {
		checks = append(checks, runtimeRequirementCheck{
			ID:      "real_codex_binary",
			Title:   "真实 Codex 二进制",
			Status:  runtimeRequirementStatusFail,
			Summary: "当前没有配置 wrapper 将要启动的真实 codex。",
		})
	} else if resolveErr != nil {
		checks = append(checks, runtimeRequirementCheck{
			ID:      "real_codex_binary",
			Title:   "真实 Codex 二进制",
			Status:  runtimeRequirementStatusFail,
			Summary: "当前服务环境下无法解析到可执行的 codex。",
			Detail:  resolveErr.Error(),
		})
	} else {
		codexReady = true
		checks = append(checks, runtimeRequirementCheck{
			ID:      "real_codex_binary",
			Title:   "真实 Codex 二进制",
			Status:  runtimeRequirementStatusPass,
			Summary: "当前服务环境下可以解析到真实 codex。",
			Detail:  resolvedRealBinary,
		})
	}

	if strings.TrimSpace(currentBinary) != "" && strings.TrimSpace(resolvedRealBinary) != "" && sameExecutablePath(currentBinary, resolvedRealBinary) {
		codexReady = false
		checks = append(checks, runtimeRequirementCheck{
			ID:      "binary_loop",
			Title:   "Wrapper 启动目标",
			Status:  runtimeRequirementStatusFail,
			Summary: "wrapper 的真实 codex 指向了当前 codex-remote 自己，会形成递归启动。",
			Detail:  resolvedRealBinary,
		})
	} else if strings.TrimSpace(resolvedRealBinary) != "" {
		checks = append(checks, runtimeRequirementCheck{
			ID:      "binary_loop",
			Title:   "Wrapper 启动目标",
			Status:  runtimeRequirementStatusPass,
			Summary: "wrapper 会启动独立的真实 codex，不会回指当前 codex-remote。",
		})
	}
	if strings.TrimSpace(resolvedClaudeBinary) == "" {
		checks = append(checks, runtimeRequirementCheck{
			ID:      "claude_binary",
			Title:   "Claude 可执行文件",
			Status:  runtimeRequirementStatusFail,
			Summary: "当前服务环境下无法解析 Claude executable。",
			Detail:  errorDetail(claudeResolveErr),
		})
	} else {
		claudeReady = true
		checks = append(checks, runtimeRequirementCheck{
			ID:      "claude_binary",
			Title:   "Claude 可执行文件",
			Status:  runtimeRequirementStatusPass,
			Summary: "当前服务环境下可以解析到 Claude executable。",
			Detail:  resolvedClaudeBinary,
		})
	}

	switch lookupMode {
	case "absolute":
		checks = append(checks, runtimeRequirementCheck{
			ID:      "lookup_mode",
			Title:   "二进制定位方式",
			Status:  runtimeRequirementStatusPass,
			Summary: "当前使用绝对路径定位真实 codex，服务环境更稳定。",
			Detail:  codexRealBinary,
		})
	case "explicit_relative":
		hasWarn = true
		checks = append(checks, runtimeRequirementCheck{
			ID:      "lookup_mode",
			Title:   "二进制定位方式",
			Status:  runtimeRequirementStatusWarn,
			Summary: "当前使用相对路径定位真实 codex，结果会依赖服务的工作目录。",
			Detail:  codexRealBinary,
		})
	case "path_search":
		hasWarn = true
		checks = append(checks, runtimeRequirementCheck{
			ID:      "lookup_mode",
			Title:   "二进制定位方式",
			Status:  runtimeRequirementStatusWarn,
			Summary: "当前通过 PATH 解析真实 codex。现在可用，但服务环境的 PATH 变化可能让它失效。",
			Detail:  codexRealBinary,
		})
	}

	ready := launcherReady && (codexReady || claudeReady)
	summary := "当前机器已满足基础运行条件，可以继续后面的可选配置。"
	switch {
	case !launcherReady:
		summary = "当前服务缺少可用启动器，请先修复后再继续。"
	case !codexReady && !claudeReady:
		summary = "当前机器还不满足基础运行条件，请先保证 Claude 或 Codex 至少一个可用。"
	case hasWarn:
		summary = "当前机器已满足基础运行条件，但仍有需要注意的配置风险。"
	}

	return runtimeRequirementsResponse{
		Ready:                   ready,
		Summary:                 summary,
		CurrentBinary:           currentBinary,
		CodexRealBinary:         codexRealBinary,
		CodexRealBinarySource:   source,
		ResolvedCodexRealBinary: resolvedRealBinary,
		LookupMode:              lookupMode,
		Checks:                  checks,
		Notes: []string{
			"这里只检查 headless 的基础可执行条件，不检查 Codex / Claude 登录状态、账号配置或 provider 凭据。",
		},
	}, nil
}

func errorDetail(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func resolvedCodexRealBinarySetting(loaded config.LoadedAppConfig) (string, string) {
	if value := strings.TrimSpace(os.Getenv("CODEX_REAL_BINARY")); value != "" {
		return value, "env_override"
	}
	if value := strings.TrimSpace(loaded.Config.Wrapper.CodexRealBinary); value != "" {
		return value, "config"
	}
	return config.DefaultAppConfig().Wrapper.CodexRealBinary, "default"
}

func codexBinaryLookupMode(value string) string {
	trimmed := strings.TrimSpace(value)
	switch {
	case trimmed == "":
		return ""
	case filepath.IsAbs(trimmed):
		return "absolute"
	case strings.Contains(trimmed, "/") || strings.Contains(trimmed, `\`):
		return "explicit_relative"
	default:
		return "path_search"
	}
}

func resolveExecutablePath(value string) (string, error) {
	resolved, err := exec.LookPath(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	return canonicalExecutablePath(resolved), nil
}

func canonicalExecutablePath(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil && strings.TrimSpace(resolved) != "" {
		path = resolved
	}
	return filepath.Clean(path)
}

func sameExecutablePath(left, right string) bool {
	left = canonicalExecutablePath(left)
	right = canonicalExecutablePath(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
