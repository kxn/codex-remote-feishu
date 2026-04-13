param(
  [string]$Version = "v0.0.0",
  [string]$BetaVersion = "v0.1.0-beta.1",
  [string]$ProdDistDir = "",
  [string]$BetaDistDir = "",
  [switch]$Help
)

$ErrorActionPreference = "Stop"

function Show-Usage {
  @'
usage: scripts/check/smoke-install-release.ps1 [options]

options:
  -Version <version>       production version fixture to test (default: v0.0.0)
  -BetaVersion <version>   beta version fixture to test (default: v0.1.0-beta.1)
  -ProdDistDir <dir>       reuse an existing production artifact directory
  -BetaDistDir <dir>       reuse an existing beta artifact directory
  -Help                    show this help
'@ | Write-Output
}

function Get-FreePort {
  $listener = New-Object System.Net.Sockets.TcpListener -ArgumentList ([System.Net.IPAddress]::Loopback), 0
  $listener.Start()
  try {
    return ([System.Net.IPEndPoint]$listener.LocalEndpoint).Port
  } finally {
    $listener.Stop()
  }
}

function Current-AssetName([string]$VersionValue) {
  return "codex-remote-feishu_{0}_windows_amd64.zip" -f $VersionValue.TrimStart("v")
}

function Get-PythonCommand {
  foreach ($candidate in @("python", "py")) {
    $command = Get-Command $candidate -ErrorAction SilentlyContinue
    if ($null -ne $command) {
      return $command.Source
    }
  }
  throw "python is required for the installer smoke test."
}

function Ensure-DistDir([string]$VersionValue, [string]$TargetDir) {
  if (-not [string]::IsNullOrWhiteSpace($TargetDir) -and (Test-Path -LiteralPath $TargetDir -PathType Container)) {
    return
  }
  if ([string]::IsNullOrWhiteSpace($TargetDir)) {
    throw "dist dir is required on Windows smoke tests when no reusable directory was provided."
  }

  $bash = Get-Command bash -ErrorAction SilentlyContinue
  if ($null -eq $bash) {
    throw "bash is required to build Windows release fixtures when dist dirs are missing."
  }

  $scriptPath = Join-Path $RootDir "scripts/release/build-artifacts.sh"
  $args = @($scriptPath, $VersionValue, $TargetDir, "--platform", "windows/amd64")
  if (Test-Path -LiteralPath (Join-Path $RootDir "internal/app/daemon/adminui/dist/index.html") -PathType Leaf) {
    $args += "--skip-admin-ui-build"
  }
  $previousFlavor = $env:CODEX_REMOTE_BUILD_FLAVOR
  $env:CODEX_REMOTE_BUILD_FLAVOR = "shipping"
  try {
    & $bash.Source @args
    if ($LASTEXITCODE -ne 0) {
      throw "build-artifacts.sh failed for $VersionValue"
    }
  } finally {
    if ($null -eq $previousFlavor) {
      Remove-Item Env:CODEX_REMOTE_BUILD_FLAVOR -ErrorAction SilentlyContinue
    } else {
      $env:CODEX_REMOTE_BUILD_FLAVOR = $previousFlavor
    }
  }
}

function Copy-CurrentPlatformAsset([string]$SourceDir, [string]$VersionValue, [string]$TargetDir) {
  $assetName = Current-AssetName $VersionValue
  $sourcePath = Join-Path $SourceDir $assetName
  if (-not (Test-Path -LiteralPath $sourcePath -PathType Leaf)) {
    throw "expected asset missing: $sourcePath"
  }
  Copy-Item -LiteralPath $sourcePath -Destination (Join-Path $TargetDir $assetName)
}

function Invoke-BootstrapState([string]$AdminUrl) {
  for ($i = 0; $i -lt 60; $i++) {
    try {
      return Invoke-RestMethod -Uri $AdminUrl -Method Get
    } catch {
      Start-Sleep -Milliseconds 500
    }
  }
  throw "bootstrap state not reachable: $AdminUrl"
}

function Stop-CodexRemoteProcesses([string]$ExecutableRoot) {
  $escapedRoot = [Regex]::Escape($ExecutableRoot)
  Get-CimInstance Win32_Process -Filter "Name = 'codex-remote.exe'" | ForEach-Object {
    if ($_.ExecutablePath -and $_.ExecutablePath -match "^${escapedRoot}") {
      Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
    }
  }
}

function Set-TestEnv([hashtable]$Values) {
  foreach ($entry in $Values.GetEnumerator()) {
    if ($null -eq $entry.Value) {
      Remove-Item ("Env:{0}" -f $entry.Key) -ErrorAction SilentlyContinue
    } else {
      Set-Item -Path ("Env:{0}" -f $entry.Key) -Value ([string]$entry.Value)
    }
  }
}

if ($Help) {
  Show-Usage
  exit 0
}

$RootDir = Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path))
$workDir = Join-Path ([System.IO.Path]::GetTempPath()) ("codex-remote-smoke-" + [Guid]::NewGuid().ToString("N"))
$distDir = Join-Path $workDir "dist"
$installRoot = Join-Path $workDir "install-root"
$trackInstallRoot = Join-Path $workDir "install-root-beta"
$homeDir = Join-Path $workDir "home"
$repoRootSentinel = Join-Path $workDir "repo-root"
$localAppData = Join-Path $homeDir "AppData\Local"
$appData = Join-Path $homeDir "AppData\Roaming"
$prodDistDir = if ([string]::IsNullOrWhiteSpace($ProdDistDir)) { Join-Path $workDir "dist-production" } else { $ProdDistDir }
$betaDistDir = if ([string]::IsNullOrWhiteSpace($BetaDistDir)) { Join-Path $workDir "dist-beta" } else { $BetaDistDir }
$serverProcess = $null
$envBackup = @{}

foreach ($name in @(
  "HOME",
  "USERPROFILE",
  "LOCALAPPDATA",
  "APPDATA",
  "CODEX_REMOTE_REPO_ROOT",
  "CODEX_REMOTE_CONFIG",
  "CODEX_REMOTE_INSTANCE_ID",
  "CODEX_REMOTE_VERSION",
  "CODEX_REMOTE_BASE_URL",
  "CODEX_REMOTE_INSTALL_ROOT",
  "CODEX_REMOTE_RELEASES_API_URL",
  "CODEX_REMOTE_SKIP_SETUP"
)) {
  $envBackup[$name] = (Get-Item ("Env:{0}" -f $name) -ErrorAction SilentlyContinue).Value
}

New-Item -ItemType Directory -Force -Path $workDir, $distDir, $homeDir, $localAppData, $appData, $repoRootSentinel | Out-Null

try {
  Ensure-DistDir $Version $prodDistDir
  Ensure-DistDir $BetaVersion $betaDistDir

  Copy-CurrentPlatformAsset $prodDistDir $Version $distDir
  Copy-CurrentPlatformAsset $betaDistDir $BetaVersion $distDir

  $releasesJson = @"
[
  {
    "url": "https://api.github.com/repos/kxn/codex-remote-feishu/releases/2",
    "assets_url": "https://api.github.com/repos/kxn/codex-remote-feishu/releases/2/assets",
    "html_url": "https://github.com/kxn/codex-remote-feishu/releases/tag/$BetaVersion",
    "id": 2,
    "tag_name": "$BetaVersion",
    "draft": false,
    "prerelease": true,
    "assets": []
  },
  {
    "url": "https://api.github.com/repos/kxn/codex-remote-feishu/releases/1",
    "assets_url": "https://api.github.com/repos/kxn/codex-remote-feishu/releases/1/assets",
    "html_url": "https://github.com/kxn/codex-remote-feishu/releases/tag/$Version",
    "id": 1,
    "tag_name": "$Version",
    "draft": false,
    "prerelease": false,
    "assets": []
  }
]
"@
  Set-Content -LiteralPath (Join-Path $distDir "releases.json") -Value $releasesJson -NoNewline

  $port = Get-FreePort
  $python = Get-PythonCommand
  $pythonArgs = if ([System.IO.Path]::GetFileNameWithoutExtension($python).ToLowerInvariant() -eq "py") {
    @("-3", "-m", "http.server", "$port", "--bind", "127.0.0.1", "--directory", $distDir)
  } else {
    @("-m", "http.server", "$port", "--bind", "127.0.0.1", "--directory", $distDir)
  }
  $serverProcess = Start-Process -FilePath $python -ArgumentList $pythonArgs -PassThru -WindowStyle Hidden
  for ($i = 0; $i -lt 40; $i++) {
    try {
      Invoke-WebRequest -Uri ("http://127.0.0.1:{0}/" -f $port) | Out-Null
      break
    } catch {
      Start-Sleep -Milliseconds 250
    }
  }
  Invoke-WebRequest -Uri ("http://127.0.0.1:{0}/" -f $port) | Out-Null

  Set-TestEnv @{
    HOME = $homeDir
    USERPROFILE = $homeDir
    LOCALAPPDATA = $localAppData
    APPDATA = $appData
    CODEX_REMOTE_REPO_ROOT = $repoRootSentinel
    CODEX_REMOTE_CONFIG = $null
    CODEX_REMOTE_INSTANCE_ID = $null
    CODEX_REMOTE_VERSION = $Version
    CODEX_REMOTE_BASE_URL = ("http://127.0.0.1:{0}" -f $port)
    CODEX_REMOTE_INSTALL_ROOT = $installRoot
    CODEX_REMOTE_RELEASES_API_URL = $null
    CODEX_REMOTE_SKIP_SETUP = $null
  }

  & (Join-Path $RootDir "install-release.ps1")
  if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
  }

  $expectedDir = Join-Path $installRoot $Version
  $binaryPath = Join-Path $localAppData "codex-remote\bin\codex-remote.exe"
  $configPath = Join-Path $homeDir ".config\codex-remote\config.json"
  $statePath = Join-Path $homeDir ".local\share\codex-remote\install-state.json"

  if (-not (Test-Path -LiteralPath $expectedDir -PathType Container)) {
    throw "expected release directory missing: $expectedDir"
  }
  if (-not (Test-Path -LiteralPath $binaryPath -PathType Leaf)) {
    throw "installed binary missing: $binaryPath"
  }
  if (-not (Test-Path -LiteralPath (Join-Path $expectedDir "QUICKSTART.md") -PathType Leaf)) {
    throw "QUICKSTART.md missing from release directory"
  }
  if (-not (Test-Path -LiteralPath (Join-Path $expectedDir "CHANGELOG.md") -PathType Leaf)) {
    throw "CHANGELOG.md missing from release directory"
  }
  if (-not (Test-Path -LiteralPath (Join-Path $installRoot "current"))) {
    throw "current release link missing"
  }

  $configPayload = Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json
  $statePayload = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
  if ($statePayload.currentTrack -ne "production") {
    throw "currentTrack=$($statePayload.currentTrack) want production"
  }
  if ($statePayload.installSource -ne "release") {
    throw "installSource=$($statePayload.installSource) want release"
  }
  if ($statePayload.currentVersion -ne $Version) {
    throw "currentVersion=$($statePayload.currentVersion) want $Version"
  }
  if ($statePayload.installedBinary -ne $binaryPath) {
    throw "installedBinary=$($statePayload.installedBinary) want $binaryPath"
  }

  & $binaryPath version | Out-Null
  if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
  }

  $bootstrapState = Invoke-BootstrapState ("http://127.0.0.1:{0}/api/setup/bootstrap-state" -f $configPayload.admin.listenPort)
  if (-not $bootstrapState.setupRequired) {
    throw "setupRequired=false want true"
  }
  if (-not $bootstrapState.session.trustedLoopback) {
    throw "trustedLoopback=false want true"
  }

  Set-TestEnv @{
    CODEX_REMOTE_VERSION = $null
    CODEX_REMOTE_BASE_URL = ("http://127.0.0.1:{0}" -f $port)
    CODEX_REMOTE_INSTALL_ROOT = $trackInstallRoot
    CODEX_REMOTE_RELEASES_API_URL = ("http://127.0.0.1:{0}/releases.json" -f $port)
  }

  & (Join-Path $RootDir "install-release.ps1") -Track beta -DownloadOnly
  if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
  }

  $betaExpectedDir = Join-Path $trackInstallRoot $BetaVersion
  $betaBinaryPath = Join-Path $betaExpectedDir "codex-remote.exe"
  if (-not (Test-Path -LiteralPath $betaExpectedDir -PathType Container)) {
    throw "beta release directory missing: $betaExpectedDir"
  }
  if (-not (Test-Path -LiteralPath $betaBinaryPath -PathType Leaf)) {
    throw "beta binary missing: $betaBinaryPath"
  }
  if (-not (Test-Path -LiteralPath (Join-Path $trackInstallRoot "current"))) {
    throw "beta current release link missing"
  }

  $betaVersionOutput = & $betaBinaryPath version
  if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
  }
  if ($betaVersionOutput -notmatch "-beta\.") {
    throw "beta binary version output was not a beta version: $betaVersionOutput"
  }
} finally {
  foreach ($entry in $envBackup.GetEnumerator()) {
    if ($null -eq $entry.Value) {
      Remove-Item ("Env:{0}" -f $entry.Key) -ErrorAction SilentlyContinue
    } else {
      Set-Item -Path ("Env:{0}" -f $entry.Key) -Value $entry.Value
    }
  }
  if ($null -ne $serverProcess -and -not $serverProcess.HasExited) {
    Stop-Process -Id $serverProcess.Id -Force -ErrorAction SilentlyContinue
  }
  Stop-CodexRemoteProcesses $localAppData
  if (Test-Path -LiteralPath $workDir) {
    Remove-Item -LiteralPath $workDir -Force -Recurse
  }
}
