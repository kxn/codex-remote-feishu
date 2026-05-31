#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

APP_FILE="deploy/macos/InstallerApp/Sources/InstallerApp.swift"
MODELS_FILE="deploy/macos/InstallerApp/Sources/InstallerModels.swift"
RESULT_MODEL_FILE="deploy/macos/InstallerApp/Sources/InstallerResultPageModel.swift"

fail() {
  echo "smoke-macos-installer-result-model: $*" >&2
  exit 1
}

require_file() {
  local path="$1"
  [[ -f "${path}" ]] || fail "required file not found: ${path}"
}

require_line() {
  local pattern="$1"
  local path="$2"
  rg -q "${pattern}" "${path}" || fail "expected pattern '${pattern}' in ${path}"
}

forbid_line() {
  local pattern="$1"
  local path="$2"
  if rg -q "${pattern}" "${path}"; then
    fail "unexpected pattern '${pattern}' in ${path}"
  fi
}

pick_swiftc() {
  if command -v xcrun >/dev/null 2>&1; then
    printf '%s\n' "xcrun swiftc"
    return
  fi
  if command -v swiftc >/dev/null 2>&1; then
    printf '%s\n' "swiftc"
    return
  fi
  return 1
}

run_structural_smoke() {
  require_file "${RESULT_MODEL_FILE}"
  require_line 'enum InstallerResultPageKind' "${RESULT_MODEL_FILE}"
  require_line 'enum InstallerResultPageActionKind' "${RESULT_MODEL_FILE}"
  require_line 'struct InstallerResultPageAction' "${RESULT_MODEL_FILE}"
  require_line 'struct InstallerResultPageModel' "${RESULT_MODEL_FILE}"
  require_line 'case result\(InstallerResultPageModel\)' "${MODELS_FILE}"
  require_line 'case \.result\(let model\)' "${APP_FILE}"
  forbid_line 'openURL\(summary\.result\.setupURL\)' "${APP_FILE}"
  forbid_line 'screenState = \.success' "${APP_FILE}"
  forbid_line 'screenState = \.failure' "${APP_FILE}"
  echo "smoke-macos-installer-result-model: structural smoke passed"
}

run_behavior_smoke() {
  local swiftc_cmd="$1"
  local temp_dir
  temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/macos-installer-result-model-XXXXXX")"
  trap 'rm -rf "'"${temp_dir}"'"' EXIT

  local harness_file="${temp_dir}/main.swift"
  local binary_file="${temp_dir}/smoke"

  cat > "${harness_file}" <<'SWIFT'
import Foundation

func assertTrue(_ condition: @autoclosure () -> Bool, _ message: String) {
    if !condition() {
        fputs("ASSERTION FAILED: \(message)\n", stderr)
        exit(1)
    }
}

func actionKinds(_ actions: [InstallerResultPageAction]) -> [InstallerResultPageActionKind] {
    actions.map(\.kind)
}

func makeProbe(mode: String, sameVersion: Bool) -> InstallerProbeResult {
    InstallerProbeResult(
        ok: true,
        mode: mode,
        statePath: "/tmp/state.json",
        configPath: "/tmp/config.json",
        currentVersion: sameVersion ? "1.6.0" : "1.5.0",
        currentTrack: "production",
        installerVersion: "1.6.0",
        sameVersion: sameVersion,
        currentInstallBinDir: "/Applications/Codex Remote",
        suggestedInstallBinDir: "/Applications/Codex Remote",
        installLocationEditable: mode == "first_install",
        serviceManager: "launchd_user",
        startupMode: "login_autostart",
        error: nil
    )
}

func makeSuccessResult(setupRequired: Bool, adminURL: String, setupURL: String, logPath: String) -> PackagedInstallResultValue {
    var result = PackagedInstallResultValue()
    result.ok = true
    result.mode = setupRequired ? "first_install" : "repair"
    result.serviceManager = "launchd_user"
    result.startupMode = "login_autostart"
    result.currentVersion = "1.6.0"
    result.currentTrack = "production"
    result.adminURL = adminURL
    result.setupURL = setupURL
    result.setupRequired = setupRequired
    result.logPath = logPath
    return result
}

let freshSetup = InstallerResultPageModel.fromSuccess(
    probe: makeProbe(mode: "first_install", sameVersion: false),
    result: makeSuccessResult(
        setupRequired: true,
        adminURL: "http://127.0.0.1:9999/",
        setupURL: "http://127.0.0.1:9999/setup",
        logPath: "/tmp/fresh-setup.log"
    )
)
assertTrue(freshSetup.kind == .freshInstallSetup, "fresh install + setup should use setup result kind")
assertTrue(freshSetup.primaryAction.kind == .continueWebSetup, "fresh install + setup should continue WebSetup")
assertTrue(actionKinds(freshSetup.auxiliaryActions) == [.openLogs], "fresh install + setup should only expose logs")

let freshReady = InstallerResultPageModel.fromSuccess(
    probe: makeProbe(mode: "first_install", sameVersion: false),
    result: makeSuccessResult(
        setupRequired: false,
        adminURL: "http://127.0.0.1:9999/",
        setupURL: "",
        logPath: "/tmp/fresh-ready.log"
    )
)
assertTrue(freshReady.kind == .freshInstallComplete, "fresh install without setup should use complete kind")
assertTrue(freshReady.primaryAction.kind == .finish, "fresh install without setup should finish")
assertTrue(actionKinds(freshReady.auxiliaryActions) == [.openAdminUI, .openLogs], "fresh install without setup should expose admin ui and logs")

let repairSameVersion = InstallerResultPageModel.fromSuccess(
    probe: makeProbe(mode: "repair", sameVersion: true),
    result: makeSuccessResult(
        setupRequired: false,
        adminURL: "http://127.0.0.1:9999/",
        setupURL: "",
        logPath: "/tmp/repair.log"
    )
)
assertTrue(repairSameVersion.kind == .repairComplete, "repair should use repair complete kind")
assertTrue(repairSameVersion.primaryAction.kind == .finish, "repair should finish")
assertTrue(actionKinds(repairSameVersion.auxiliaryActions) == [.openAdminUI, .openLogs], "repair should expose admin ui and logs")

let failure = InstallerResultPageModel.fromFailure(
    message: "安装失败",
    detail: "daemon restart failed",
    logPath: "/tmp/failure.log"
)
assertTrue(failure.kind == .failure, "failure should use failure kind")
assertTrue(failure.primaryAction.kind == .finish, "failure should finish")
assertTrue(actionKinds(failure.auxiliaryActions) == [.openLogs], "failure should only expose logs")
assertTrue(failure.detail.contains("daemon restart failed"), "failure should preserve detail text")
SWIFT

  require_file "${RESULT_MODEL_FILE}"
  # shellcheck disable=SC2086
  ${swiftc_cmd} \
    "${MODELS_FILE}" \
    "${RESULT_MODEL_FILE}" \
    "${harness_file}" \
    -o "${binary_file}"
  "${binary_file}"
  echo "smoke-macos-installer-result-model: behavior smoke passed"
}

swiftc_cmd=""
if swiftc_cmd="$(pick_swiftc)"; then
  run_behavior_smoke "${swiftc_cmd}"
else
  run_structural_smoke
fi
