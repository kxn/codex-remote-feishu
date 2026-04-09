#!/usr/bin/env python3

from __future__ import annotations

import argparse
import dataclasses
import fcntl
import ipaddress
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time
from typing import Iterable


DEFAULT_SERVICE_NAME = "codex-remote.service"
DEFAULT_JOURNAL_LINES = 80
DEFAULT_TIMEOUT_SECONDS = 30
PROXY_ENV_KEYS = (
    "http_proxy",
    "https_proxy",
    "HTTP_PROXY",
    "HTTPS_PROXY",
    "ALL_PROXY",
    "all_proxy",
)


@dataclasses.dataclass(frozen=True)
class Layout:
    base_dir: Path
    state_path: Path
    config_path: Path
    unit_path: Path
    installed_binary: Path
    install_bin_dir: Path
    backup_root: Path
    service_name: str
    relay_port: int
    admin_port: int
    health_url: str
    lock_path: Path


@dataclasses.dataclass(frozen=True)
class Snapshot:
    backup_dir: Path
    manifest_path: Path
    current_binary_backup: Path
    staged_artifact: Path
    state_backup: Path
    config_backup: Path
    unit_backup: Path
    had_unit: bool


def main() -> int:
    args = parse_args()
    artifact_path = Path(args.artifact).expanduser().resolve()
    if not artifact_path.is_file():
        print(f"artifact not found: {artifact_path}", file=sys.stderr)
        return 2

    try:
        layout = resolve_layout(args)
        with upgrade_lock(layout.lock_path):
            current_version = binary_version(layout.installed_binary)
            target_version = binary_version(artifact_path)
            print(f"current version: {current_version}")
            print(f"target version: {target_version}")
            print(f"installed binary: {layout.installed_binary}")

            if not args.allow_unhealthy_current:
                verify_runtime(layout, args.timeout_seconds, args)
                print("current runtime: healthy")
            else:
                print("current runtime check: skipped by --allow-unhealthy-current")

            snapshot = create_snapshot(layout, artifact_path, current_version, target_version)
            print(f"backup dir: {snapshot.backup_dir}")

            try:
                run_upgrade(layout, snapshot.staged_artifact, args)
                verify_runtime(layout, args.timeout_seconds, args)
            except Exception as exc:
                print(f"upgrade failed: {exc}", file=sys.stderr)
                rollback(layout, snapshot, args)
                try:
                    verify_runtime(layout, args.timeout_seconds, args)
                except Exception as rollback_exc:
                    print(f"rollback failed: {rollback_exc}", file=sys.stderr)
                    print(journal_tail(layout.service_name, args.journalctl_bin, args.journal_lines), file=sys.stderr)
                    return 1
                print("rollback: restored previous runtime")
                print(journal_tail(layout.service_name, args.journalctl_bin, args.journal_lines), file=sys.stderr)
                return 1

            print("upgrade: success")
            print(f"backup retained at: {snapshot.backup_dir}")
            return 0
    except Exception as exc:
        print(str(exc), file=sys.stderr)
        return 1


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Upgrade the codex-remote systemd --user deployment from a built artifact with rollback on failure."
    )
    parser.add_argument("artifact", help="path to the built codex-remote artifact")
    parser.add_argument(
        "--state-path",
        default=str(Path.home() / ".local" / "share" / "codex-remote" / "install-state.json"),
        help="path to install-state.json for the systemd deployment",
    )
    parser.add_argument("--backup-root", help="directory for backup bundles")
    parser.add_argument("--health-url", help="override localhost health endpoint")
    parser.add_argument("--service-name", default="", help="systemd user unit name override")
    parser.add_argument(
        "--timeout-seconds",
        type=int,
        default=DEFAULT_TIMEOUT_SECONDS,
        help="seconds to wait for runtime health checks",
    )
    parser.add_argument(
        "--allow-unhealthy-current",
        action="store_true",
        help="skip the pre-upgrade health requirement and use the script for recovery work",
    )
    parser.add_argument("--systemctl-bin", default="systemctl", help="systemctl executable")
    parser.add_argument("--ss-bin", default="ss", help="ss executable")
    parser.add_argument("--curl-bin", default="curl", help="curl executable")
    parser.add_argument("--journalctl-bin", default="journalctl", help="journalctl executable")
    parser.add_argument(
        "--journal-lines",
        type=int,
        default=DEFAULT_JOURNAL_LINES,
        help="journal lines to print when upgrade or rollback fails",
    )
    return parser.parse_args()


def resolve_layout(args: argparse.Namespace) -> Layout:
    state_path = Path(args.state_path).expanduser().resolve()
    state = load_json(state_path)
    service_manager = str(state.get("serviceManager", "")).strip()
    if service_manager != "systemd_user":
        raise RuntimeError(f"state serviceManager is {service_manager!r}, expected 'systemd_user'")

    base_dir = infer_base_dir(state, state_path)
    expected_state_path = base_dir / ".local" / "share" / "codex-remote" / "install-state.json"
    if state_path != expected_state_path:
        raise RuntimeError(
            f"state path {state_path} is not the default layout for base dir {base_dir}; "
            "this skill only supports default install-state paths"
        )

    config_path = Path(first_non_empty(state.get("configPath"), str(base_dir / ".config" / "codex-remote" / "config.json"))).expanduser().resolve()
    unit_path = Path(first_non_empty(state.get("serviceUnitPath"), str(base_dir / ".config" / "systemd" / "user" / DEFAULT_SERVICE_NAME))).expanduser().resolve()
    installed_binary = Path(first_non_empty(state.get("installedBinary"), state.get("currentBinaryPath"))).expanduser().resolve()
    if not installed_binary.is_file():
        raise RuntimeError(f"installed binary not found: {installed_binary}")

    config = load_json(config_path)
    relay_port = int(config.get("relay", {}).get("listenPort") or 9500)
    admin_port = int(config.get("admin", {}).get("listenPort") or 9501)
    admin_host = normalize_local_host(str(config.get("admin", {}).get("listenHost") or "127.0.0.1"))
    health_url = args.health_url or f"http://{admin_host}:{admin_port}/v1/status"
    backup_root = Path(args.backup_root).expanduser().resolve() if args.backup_root else (base_dir / ".local" / "share" / "codex-remote" / "systemd-user-upgrade-backups")
    lock_path = backup_root / ".upgrade.lock"

    service_name = args.service_name or unit_path.name or DEFAULT_SERVICE_NAME

    return Layout(
        base_dir=base_dir,
        state_path=state_path,
        config_path=config_path,
        unit_path=unit_path,
        installed_binary=installed_binary,
        install_bin_dir=installed_binary.parent,
        backup_root=backup_root,
        service_name=service_name,
        relay_port=relay_port,
        admin_port=admin_port,
        health_url=health_url,
        lock_path=lock_path,
    )


def infer_base_dir(state: dict, state_path: Path) -> Path:
    explicit = str(state.get("baseDir", "")).strip()
    if explicit:
        return Path(explicit).expanduser().resolve()
    return state_path.parents[3]


def first_non_empty(*values: object) -> str:
    for value in values:
        text = str(value or "").strip()
        if text:
            return text
    return ""


def load_json(path: Path) -> dict:
    with path.open("r", encoding="utf-8") as handle:
        return json.load(handle)


def normalize_local_host(host: str) -> str:
    host = host.strip().strip("[]")
    if host in {"", "localhost", "0.0.0.0", "::"}:
        return "127.0.0.1"
    try:
        ip = ipaddress.ip_address(host)
    except ValueError:
        return host
    if ip.is_loopback or ip.is_unspecified:
        return "127.0.0.1"
    return host


def clean_local_env() -> dict[str, str]:
    env = os.environ.copy()
    for key in PROXY_ENV_KEYS:
        env.pop(key, None)
    return env


def binary_version(path: Path) -> str:
    result = run_command([str(path), "version"], env=os.environ.copy())
    return result.stdout.strip()


def run_upgrade(layout: Layout, staged_artifact: Path, args: argparse.Namespace) -> None:
    run_command([args.systemctl_bin, "--user", "stop", layout.service_name], env=clean_local_env())
    command = [
        str(staged_artifact),
        "install",
        "-bootstrap-only",
        "-base-dir",
        str(layout.base_dir),
        "-install-bin-dir",
        str(layout.install_bin_dir),
        "-service-manager",
        "systemd_user",
        "-start-daemon",
    ]
    run_command(command, env=os.environ.copy())


def verify_runtime(layout: Layout, timeout_seconds: int, args: argparse.Namespace) -> None:
    deadline = time.monotonic() + timeout_seconds
    last_error = "runtime check did not run"
    while time.monotonic() <= deadline:
        problems: list[str] = []
        if not service_is_active(layout.service_name, args.systemctl_bin):
            problems.append("systemd user service is not active")
        if not ports_are_listening((layout.relay_port, layout.admin_port), args.ss_bin):
            problems.append(f"expected ports {layout.relay_port}/{layout.admin_port} are not both listening")
        if not healthcheck_ok(layout.health_url, args.curl_bin):
            problems.append(f"health endpoint failed: {layout.health_url}")
        if not problems:
            return
        last_error = "; ".join(problems)
        time.sleep(1)
    raise RuntimeError(last_error)


def service_is_active(service_name: str, systemctl_bin: str) -> bool:
    result = run_command(
        [systemctl_bin, "--user", "is-active", service_name],
        env=clean_local_env(),
        check=False,
    )
    return result.returncode == 0 and result.stdout.strip() == "active"


def ports_are_listening(ports: Iterable[int], ss_bin: str) -> bool:
    result = run_command([ss_bin, "-lntp"], env=clean_local_env(), check=False)
    output = result.stdout
    return all(f":{port}" in output for port in ports)


def healthcheck_ok(url: str, curl_bin: str) -> bool:
    result = run_command(
        [curl_bin, "--noproxy", "*", "-fsS", url],
        env=clean_local_env(),
        check=False,
    )
    if result.returncode != 0:
        return False
    body = result.stdout.strip()
    if not body:
        return False
    try:
        json.loads(body)
    except json.JSONDecodeError:
        return False
    return True


def create_snapshot(layout: Layout, artifact_path: Path, current_version: str, target_version: str) -> Snapshot:
    backup_dir = unique_backup_dir(layout.backup_root)
    timestamp = backup_dir.name
    staged_dir = backup_dir / "staged"
    staged_dir.mkdir(parents=True, exist_ok=True)

    current_binary_backup = backup_dir / "current-binary" / layout.installed_binary.name
    state_backup = backup_dir / "state" / layout.state_path.name
    config_backup = backup_dir / "config" / layout.config_path.name
    unit_backup = backup_dir / "unit" / layout.unit_path.name
    staged_artifact = staged_dir / layout.installed_binary.name

    copy_file(layout.installed_binary, current_binary_backup)
    copy_file(layout.state_path, state_backup)
    copy_file(layout.config_path, config_backup)
    had_unit = layout.unit_path.exists()
    if had_unit:
        copy_file(layout.unit_path, unit_backup)
    copy_file(artifact_path, staged_artifact)

    manifest_path = backup_dir / "manifest.json"
    manifest = {
        "createdAt": timestamp,
        "currentVersion": current_version,
        "targetVersion": target_version,
        "installedBinary": str(layout.installed_binary),
        "statePath": str(layout.state_path),
        "configPath": str(layout.config_path),
        "unitPath": str(layout.unit_path),
        "stagedArtifact": str(staged_artifact),
        "serviceName": layout.service_name,
        "relayPort": layout.relay_port,
        "adminPort": layout.admin_port,
    }
    manifest_path.write_text(json.dumps(manifest, ensure_ascii=True, indent=2) + "\n", encoding="utf-8")

    return Snapshot(
        backup_dir=backup_dir,
        manifest_path=manifest_path,
        current_binary_backup=current_binary_backup,
        staged_artifact=staged_artifact,
        state_backup=state_backup,
        config_backup=config_backup,
        unit_backup=unit_backup,
        had_unit=had_unit,
    )


def unique_backup_dir(root: Path) -> Path:
    root.mkdir(parents=True, exist_ok=True)
    stamp = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())
    for attempt in range(1000):
        suffix = "" if attempt == 0 else f"-{attempt:03d}"
        candidate = root / f"{stamp}{suffix}"
        try:
            candidate.mkdir(parents=True, exist_ok=False)
        except FileExistsError:
            continue
        return candidate
    raise RuntimeError(f"unable to allocate backup directory under {root}")


def rollback(layout: Layout, snapshot: Snapshot, args: argparse.Namespace) -> None:
    run_command([args.systemctl_bin, "--user", "stop", layout.service_name], env=clean_local_env(), check=False)
    restore_file(snapshot.current_binary_backup, layout.installed_binary)
    restore_file(snapshot.state_backup, layout.state_path)
    restore_file(snapshot.config_backup, layout.config_path)
    if snapshot.had_unit:
        restore_file(snapshot.unit_backup, layout.unit_path)
    elif layout.unit_path.exists():
        layout.unit_path.unlink()
    run_command([args.systemctl_bin, "--user", "daemon-reload"], env=clean_local_env(), check=False)
    run_command([args.systemctl_bin, "--user", "reset-failed", layout.service_name], env=clean_local_env(), check=False)
    run_command([args.systemctl_bin, "--user", "enable", layout.service_name], env=clean_local_env(), check=False)
    run_command([args.systemctl_bin, "--user", "start", layout.service_name], env=clean_local_env())


def journal_tail(service_name: str, journalctl_bin: str, lines: int) -> str:
    result = run_command(
        [journalctl_bin, "--user", "-u", service_name, "-n", str(lines), "--no-pager", "--output=short-iso"],
        env=clean_local_env(),
        check=False,
    )
    return result.stdout.strip()


def copy_file(source: Path, target: Path) -> None:
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, target)


def restore_file(source: Path, target: Path) -> None:
    if not source.exists():
        raise RuntimeError(f"backup missing: {source}")
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, target)


def run_command(command: list[str], *, env: dict[str, str], check: bool = True) -> subprocess.CompletedProcess[str]:
    result = subprocess.run(command, env=env, text=True, capture_output=True, check=False)
    if check and result.returncode != 0:
        message = (
            f"command failed ({result.returncode}): {' '.join(command)}\n"
            f"stdout:\n{result.stdout}\n"
            f"stderr:\n{result.stderr}"
        )
        raise RuntimeError(message.rstrip())
    return result


class upgrade_lock:
    def __init__(self, lock_path: Path):
        self.lock_path = lock_path
        self.handle: os.PathLike[str] | None = None

    def __enter__(self) -> "upgrade_lock":
        self.lock_path.parent.mkdir(parents=True, exist_ok=True)
        self.handle = self.lock_path.open("w", encoding="utf-8")
        try:
            fcntl.flock(self.handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as exc:
            raise RuntimeError(f"another systemd upgrade is already running: {self.lock_path}") from exc
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        assert self.handle is not None
        fcntl.flock(self.handle.fileno(), fcntl.LOCK_UN)
        self.handle.close()


if __name__ == "__main__":
    sys.exit(main())
