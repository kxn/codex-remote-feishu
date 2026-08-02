param(
  [string]$Version = "v0.0.0",
  [string]$UpgradeVersion = "v0.1.0-beta.1",
  [string]$UpgradeTrack = "beta",
  [string]$ProdDistDir = "",
  [string]$UpgradeDistDir = "",
  [switch]$Help
)

$ErrorActionPreference = "Stop"

function Show-Usage {
  @'
usage: scripts/check/smoke-packaged-install-lifecycle.ps1 [options]

options:
  -Version <version>          first-install version fixture
  -UpgradeVersion <version>   upgrade version fixture
  -UpgradeTrack <track>       upgrade fixture track
  -ProdDistDir <dir>          production artifact directory
  -UpgradeDistDir <dir>       upgrade artifact directory
  -Help                       show this help
'@ | Write-Output
}

function Fail([string]$Message) {
  throw "smoke-packaged-install-lifecycle: $Message"
}

function Get-AssetName([string]$VersionValue) {
  return ("codex-remote-feishu_{0}_windows_amd64.zip" -f $VersionValue.TrimStart("v"))
}

function Expand-Binary([string]$DistDir, [string]$VersionValue, [string]$OutputDir) {
  $asset = Join-Path $DistDir (Get-AssetName $VersionValue)
  if (-not (Test-Path -LiteralPath $asset -PathType Leaf)) {
    Fail "expected artifact missing: $asset"
  }
  New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null
  Expand-Archive -LiteralPath $asset -DestinationPath $OutputDir -Force
  $binary = Get-ChildItem -LiteralPath $OutputDir -Recurse -File -Filter "codex-remote.exe" | Select-Object -First 1
  if ($null -eq $binary) {
    Fail "codex-remote.exe not found after extracting $asset"
  }
  return $binary.FullName
}

function Get-FreePort {
  $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Parse("127.0.0.1"), 0)
  $listener.Start()
  try {
    return $listener.LocalEndpoint.Port
  } finally {
    $listener.Stop()
  }
}

function Assert-JsonField([string]$PathValue, [string]$Field, [string]$Expected) {
  $payload = Get-Content -LiteralPath $PathValue -Raw | ConvertFrom-Json
  $actual = $payload
  foreach ($part in $Field.Split(".")) {
    $actual = $actual.$part
  }
  if ([string]$actual -cne $Expected) {
    throw "$Field mismatch. actual=[$actual] expected=[$Expected]"
  }
}

function Stop-CodexRemoteProcesses([string]$ExecutableRoot) {
  $escapedRoot = [Regex]::Escape($ExecutableRoot)
  Get-CimInstance Win32_Process -Filter "Name = 'codex-remote.exe'" | ForEach-Object {
    if ($_.ExecutablePath -and $_.ExecutablePath -match "^${escapedRoot}") {
      Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
    }
  }
}

if ($Help) {
  Show-Usage
  exit 0
}

if ([string]::IsNullOrWhiteSpace($ProdDistDir)) {
  Fail "-ProdDistDir is required"
}
if ([string]::IsNullOrWhiteSpace($UpgradeDistDir)) {
  Fail "-UpgradeDistDir is required"
}

$workDir = Join-Path ([System.IO.Path]::GetTempPath()) ("codex-remote-packaged-lifecycle-" + [Guid]::NewGuid().ToString("N"))
$baseDir = Join-Path $workDir "home"
$installBinDir = Join-Path $workDir "install-bin"
$statePath = Join-Path $baseDir ".local\share\codex-remote\install-state.json"
$configDir = Join-Path $baseDir ".config\codex-remote"
$taskXmlPath = Join-Path $baseDir ".local\share\codex-remote\task-scheduler-logon.xml"
$taskName = "\CodexRemoteFeishu\stable"
$resultDir = Join-Path $workDir "results"
$envBackup = @{}

foreach ($name in @("HOME", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME")) {
  $envBackup[$name] = (Get-Item ("Env:{0}" -f $name) -ErrorAction SilentlyContinue).Value
}

New-Item -ItemType Directory -Force -Path $workDir, $baseDir, $configDir, $resultDir | Out-Null

try {
  $prodBinary = Expand-Binary $ProdDistDir $Version (Join-Path $workDir "prod")
  $upgradeBinary = Expand-Binary $UpgradeDistDir $UpgradeVersion (Join-Path $workDir "upgrade")

  $relayPort = Get-FreePort
  $adminPort = Get-FreePort
  $toolPort = Get-FreePort
  $externalPort = Get-FreePort
  $configJson = @"
{
  "version": 1,
  "relay": {
    "listenHost": "127.0.0.1",
    "listenPort": $relayPort,
    "serverURL": "ws://127.0.0.1:$relayPort/ws/agent"
  },
  "admin": {
    "listenHost": "127.0.0.1",
    "listenPort": $adminPort,
    "autoOpenBrowser": false
  },
  "tool": {
    "listenHost": "127.0.0.1",
    "listenPort": $toolPort
  },
  "externalAccess": {
    "listenHost": "127.0.0.1",
    "listenPort": $externalPort
  },
  "wrapper": {
    "codexRealBinary": "codex",
    "nameMode": "workspace_basename",
    "integrationMode": "none"
  },
  "feishu": {
    "useSystemProxy": false,
    "apps": []
  },
  "debug": {},
  "storage": {
    "previewRootFolderName": "Codex Remote Previews"
  }
}
"@
  Set-Content -LiteralPath (Join-Path $configDir "config.json") -Value $configJson -NoNewline

  $env:HOME = $baseDir
  $env:USERPROFILE = $baseDir
  $env:LOCALAPPDATA = Join-Path $baseDir "AppData\Local"
  $env:APPDATA = Join-Path $baseDir "AppData\Roaming"
  $env:XDG_CONFIG_HOME = Join-Path $baseDir ".config"
  $env:XDG_DATA_HOME = Join-Path $baseDir ".local\share"
  $env:XDG_STATE_HOME = Join-Path $baseDir ".local\state"

  & $prodBinary packaged-install `
    -base-dir $baseDir `
    -install-bin-dir $installBinDir `
    -binary $prodBinary `
    -install-source release `
    -current-version $Version `
    -current-track production `
    -format json `
    -result-file (Join-Path $resultDir "first-install.ini") | Set-Content -LiteralPath (Join-Path $resultDir "first-install.json")
  if ($LASTEXITCODE -ne 0) {
    Fail "first install failed with exit code $LASTEXITCODE"
  }

  $liveBinary = Join-Path $installBinDir "codex-remote.exe"
  if (-not (Test-Path -LiteralPath $liveBinary -PathType Leaf)) {
    Fail "live binary missing after first install: $liveBinary"
  }
  if (-not (Test-Path -LiteralPath $statePath -PathType Leaf)) {
    Fail "install state missing after first install: $statePath"
  }
  if (-not (Test-Path -LiteralPath $taskXmlPath -PathType Leaf)) {
    Fail "Task Scheduler XML missing after first install: $taskXmlPath"
  }
  schtasks /Query /TN $taskName | Out-Null
  if ($LASTEXITCODE -ne 0) {
    Fail "Task Scheduler task missing after first install: $taskName"
  }
  Assert-JsonField $statePath "currentVersion" $Version
  Assert-JsonField $statePath "currentTrack" "production"
  Assert-JsonField $statePath "serviceManager" "task_scheduler_logon"

  $versionOutput = & $liveBinary version
  if ($LASTEXITCODE -ne 0 -or $versionOutput.Trim() -ne $Version) {
    Fail "installed binary version mismatch: $versionOutput"
  }

  & $liveBinary packaged-install-probe `
    -base-dir $baseDir `
    -current-version $Version `
    -format json | Set-Content -LiteralPath (Join-Path $resultDir "repair-probe.json")
  if ($LASTEXITCODE -ne 0) {
    Fail "repair probe failed with exit code $LASTEXITCODE"
  }
  Assert-JsonField (Join-Path $resultDir "repair-probe.json") "mode" "repair"
  Assert-JsonField (Join-Path $resultDir "repair-probe.json") "sameVersion" "True"

  & $liveBinary packaged-install `
    -state-path $statePath `
    -binary $prodBinary `
    -install-source release `
    -current-version $Version `
    -current-track production `
    -format json `
    -result-file (Join-Path $resultDir "repair.ini") | Set-Content -LiteralPath (Join-Path $resultDir "repair.json")
  if ($LASTEXITCODE -ne 0) {
    Fail "repair install failed with exit code $LASTEXITCODE"
  }
  Assert-JsonField (Join-Path $resultDir "repair.json") "mode" "repair"
  Assert-JsonField $statePath "currentVersion" $Version

  & $liveBinary packaged-install `
    -state-path $statePath `
    -binary $upgradeBinary `
    -install-source release `
    -current-version $UpgradeVersion `
    -current-track $UpgradeTrack `
    -format json `
    -result-file (Join-Path $resultDir "upgrade.ini") | Set-Content -LiteralPath (Join-Path $resultDir "upgrade.json")
  if ($LASTEXITCODE -ne 0) {
    Fail "upgrade install failed with exit code $LASTEXITCODE"
  }
  Assert-JsonField (Join-Path $resultDir "upgrade.json") "mode" "repair"
  Assert-JsonField $statePath "currentVersion" $UpgradeVersion
  Assert-JsonField $statePath "currentTrack" $UpgradeTrack

  $upgradeVersionOutput = & $liveBinary version
  if ($LASTEXITCODE -ne 0 -or $upgradeVersionOutput.Trim() -ne $UpgradeVersion) {
    Fail "upgraded binary version mismatch: $upgradeVersionOutput"
  }

  & $liveBinary service uninstall-user -state-path $statePath | Set-Content -LiteralPath (Join-Path $resultDir "uninstall.txt")
  if ($LASTEXITCODE -ne 0) {
    Fail "service uninstall-user failed with exit code $LASTEXITCODE"
  }
  Assert-JsonField $statePath "serviceManager" "detached"
  if (Test-Path -LiteralPath $taskXmlPath) {
    Fail "Task Scheduler XML still exists after uninstall: $taskXmlPath"
  }
  schtasks /Query /TN $taskName | Out-Null
  if ($LASTEXITCODE -eq 0) {
    Fail "Task Scheduler task still exists after uninstall: $taskName"
  }

  Write-Output "packaged installer lifecycle smoke passed"
} finally {
  $cleanupBinary = Join-Path $installBinDir "codex-remote.exe"
  if ((Test-Path -LiteralPath $statePath -PathType Leaf) -and (Test-Path -LiteralPath $cleanupBinary -PathType Leaf)) {
    & $cleanupBinary service uninstall-user -state-path $statePath *> $null
  }
  schtasks /Delete /TN $taskName /F *> $null
  Stop-CodexRemoteProcesses $installBinDir
  foreach ($entry in $envBackup.GetEnumerator()) {
    if ($null -eq $entry.Value) {
      Remove-Item ("Env:{0}" -f $entry.Key) -ErrorAction SilentlyContinue
    } else {
      Set-Item -Path ("Env:{0}" -f $entry.Key) -Value $entry.Value
    }
  }
  if (Test-Path -LiteralPath $workDir) {
    Remove-Item -LiteralPath $workDir -Force -Recurse -ErrorAction SilentlyContinue
  }
}
