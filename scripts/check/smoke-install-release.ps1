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

function Get-GoCommand {
  $command = Get-Command go -ErrorAction SilentlyContinue
  if ($null -eq $command) {
    throw "go is required to build Windows installer smoke fixtures when prebuilt dist dirs are missing."
  }
  return $command.Source
}

function Get-NpmCommand {
  foreach ($candidate in @("npm.cmd", "npm")) {
    $command = Get-Command $candidate -ErrorAction SilentlyContinue
    if ($null -ne $command) {
      return $command.Source
    }
  }
  throw "npm is required to build the admin UI when the embedded dist is missing."
}

function Get-BuildBranch {
  if (-not [string]::IsNullOrWhiteSpace($env:CODEX_REMOTE_BUILD_BRANCH)) {
    return $env:CODEX_REMOTE_BUILD_BRANCH.Trim()
  }

  $git = Get-Command git -ErrorAction SilentlyContinue
  if ($null -ne $git) {
    $branch = & $git.Source branch --show-current 2>$null
    if ($LASTEXITCODE -eq 0) {
      $resolved = [string]($branch | Select-Object -First 1)
      if (-not [string]::IsNullOrWhiteSpace($resolved)) {
        return $resolved.Trim()
      }
    }
  }

  if (-not [string]::IsNullOrWhiteSpace($env:GITHUB_REF_NAME)) {
    return $env:GITHUB_REF_NAME.Trim()
  }
  return "dev"
}

function Ensure-AdminUiDist {
  $distIndexPath = Join-Path $RootDir "internal/app/daemon/adminui/dist/index.html"
  if (Test-Path -LiteralPath $distIndexPath -PathType Leaf) {
    return
  }

  $npm = Get-NpmCommand
  $webDir = Join-Path $RootDir "web"
  if (-not (Test-Path -LiteralPath $webDir -PathType Container)) {
    throw "web directory missing: $webDir"
  }

  Push-Location $webDir
  try {
    if (Test-Path -LiteralPath (Join-Path $webDir "package-lock.json") -PathType Leaf) {
      & $npm ci
      if ($LASTEXITCODE -ne 0) {
        throw "npm ci failed while building admin UI fixtures."
      }
    } else {
      & $npm install
      if ($LASTEXITCODE -ne 0) {
        throw "npm install failed while building admin UI fixtures."
      }
    }

    & $npm run build
    if ($LASTEXITCODE -ne 0) {
      throw "npm run build failed while building admin UI fixtures."
    }
  } finally {
    Pop-Location
  }

  if (-not (Test-Path -LiteralPath $distIndexPath -PathType Leaf)) {
    throw "admin UI dist is still missing after npm run build."
  }
}

function Build-WindowsReleaseFixture([string]$VersionValue, [string]$TargetDir) {
  $go = Get-GoCommand
  Ensure-AdminUiDist
  New-Item -ItemType Directory -Force -Path $TargetDir | Out-Null

  $packageName = "codex-remote-feishu_{0}_windows_amd64" -f $VersionValue.TrimStart("v")
  $archivePath = Join-Path $TargetDir (Current-AssetName $VersionValue)
  $buildRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("codex-remote-release-build-" + [Guid]::NewGuid().ToString("N"))
  $stagingDir = Join-Path $buildRoot $packageName
  $binaryPath = Join-Path $stagingDir "codex-remote.exe"
  $previousEnv = @{
    "CGO_ENABLED" = (Get-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue).Value
    "GOOS" = (Get-Item Env:GOOS -ErrorAction SilentlyContinue).Value
    "GOARCH" = (Get-Item Env:GOARCH -ErrorAction SilentlyContinue).Value
  }

  New-Item -ItemType Directory -Force -Path $stagingDir | Out-Null
  try {
    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    $ldflags = "-X main.version=$VersionValue -X main.branch=$(Get-BuildBranch) -X github.com/kxn/codex-remote-feishu/internal/buildinfo.FlavorValue=shipping"

    Push-Location $RootDir
    try {
      & $go build -buildvcs=false -trimpath -ldflags $ldflags -o $binaryPath ./cmd/codex-remote
      if ($LASTEXITCODE -ne 0) {
        throw "go build failed for $VersionValue"
      }
    } finally {
      Pop-Location
    }

    Copy-Item -LiteralPath (Join-Path $RootDir "QUICKSTART.md") -Destination (Join-Path $stagingDir "QUICKSTART.md")
    Copy-Item -LiteralPath (Join-Path $RootDir "CHANGELOG.md") -Destination (Join-Path $stagingDir "CHANGELOG.md")

    Push-Location $buildRoot
    try {
      Compress-Archive -Path $packageName -DestinationPath $archivePath -Force
    } finally {
      Pop-Location
    }
  } finally {
    foreach ($entry in $previousEnv.GetEnumerator()) {
      if ($null -eq $entry.Value) {
        Remove-Item ("Env:{0}" -f $entry.Key) -ErrorAction SilentlyContinue
      } else {
        Set-Item -Path ("Env:{0}" -f $entry.Key) -Value $entry.Value
      }
    }
    if (Test-Path -LiteralPath $buildRoot) {
      Remove-Item -LiteralPath $buildRoot -Force -Recurse -ErrorAction SilentlyContinue
    }
  }

  if (-not (Test-Path -LiteralPath $archivePath -PathType Leaf)) {
    throw "failed to create Windows release fixture: $archivePath"
  }
}

function Ensure-SystemNetHttp {
  if ("System.Net.Http.HttpClientHandler" -as [type]) {
    return
  }

  try {
    Add-Type -AssemblyName System.Net.Http | Out-Null
  } catch {
    throw "failed to load System.Net.Http for smoke-install-release.ps1. $($_.Exception.Message)"
  }

  if (-not ("System.Net.Http.HttpClientHandler" -as [type])) {
    throw "System.Net.Http is unavailable in this PowerShell session."
  }
}

function Invoke-TextRequest([string]$Url) {
  Ensure-SystemNetHttp
  $handler = New-Object System.Net.Http.HttpClientHandler
  if ($Url -match '^https?://(127\.0\.0\.1|localhost)(:\d+)?(/|$)') {
    $handler.UseProxy = $false
  }
  $client = New-Object System.Net.Http.HttpClient -ArgumentList $handler
  $client.Timeout = [TimeSpan]::FromSeconds(5)
  $client.DefaultRequestHeaders.UserAgent.ParseAdd("codex-remote-smoke-install")

  try {
    $response = $client.GetAsync($Url).GetAwaiter().GetResult()
    try {
      [void]$response.EnsureSuccessStatusCode()
      return $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
    } finally {
      $response.Dispose()
    }
  } finally {
    $client.Dispose()
    $handler.Dispose()
  }
}

function Ensure-DistDir([string]$VersionValue, [string]$TargetDir) {
  if ([string]::IsNullOrWhiteSpace($TargetDir)) {
    throw "dist dir is required on Windows smoke tests when no reusable directory was provided."
  }

  $assetPath = Join-Path $TargetDir (Current-AssetName $VersionValue)
  if (Test-Path -LiteralPath $assetPath -PathType Leaf) {
    return
  }

  Build-WindowsReleaseFixture $VersionValue $TargetDir
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
      return Invoke-TextRequest $AdminUrl | ConvertFrom-Json
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
  return
}

$RootDir = Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path))
$workDir = Join-Path ([System.IO.Path]::GetTempPath()) ("codex-remote-smoke-" + [Guid]::NewGuid().ToString("N"))
$distDir = Join-Path $workDir "dist"
$installRoot = Join-Path $workDir "install-root"
$trackInstallRoot = Join-Path $workDir "install-root-beta"
$homeDir = Join-Path $workDir "home"
$repoRootSentinel = Join-Path $workDir "repo-root"
$xdgConfigHome = Join-Path $homeDir ".config"
$xdgDataHome = Join-Path $homeDir ".local\share"
$xdgStateHome = Join-Path $homeDir ".local\state"
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
  "XDG_CONFIG_HOME",
  "XDG_DATA_HOME",
  "XDG_STATE_HOME",
  "CODEX_REMOTE_REPO_ROOT",
  "CODEX_REMOTE_CONFIG",
  "CODEX_REMOTE_INSTANCE_ID",
  "CODEX_REMOTE_VERSION",
  "CODEX_REMOTE_BASE_URL",
  "CODEX_REMOTE_INSTALL_ROOT",
  "CODEX_REMOTE_RELEASES_API_URL",
  "CODEX_REMOTE_SKIP_SETUP",
  "http_proxy",
  "https_proxy",
  "all_proxy"
)) {
  $envBackup[$name] = (Get-Item ("Env:{0}" -f $name) -ErrorAction SilentlyContinue).Value
}

New-Item -ItemType Directory -Force -Path $workDir, $distDir, $homeDir, $xdgConfigHome, $xdgDataHome, $xdgStateHome, $localAppData, $appData, $repoRootSentinel | Out-Null

try {
  foreach ($proxyName in @("http_proxy", "https_proxy", "all_proxy")) {
    Remove-Item ("Env:{0}" -f $proxyName) -ErrorAction SilentlyContinue
  }

  $relayPort = Get-FreePort
  $adminPort = Get-FreePort
  $toolPort = Get-FreePort
  $externalAccessPort = Get-FreePort
  $configDir = Join-Path $xdgConfigHome "codex-remote"
  New-Item -ItemType Directory -Force -Path $configDir | Out-Null
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
    "listenPort": $externalAccessPort
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
      [void](Invoke-TextRequest ("http://127.0.0.1:{0}/" -f $port))
      break
    } catch {
      Start-Sleep -Milliseconds 250
    }
  }
  [void](Invoke-TextRequest ("http://127.0.0.1:{0}/" -f $port))

  Set-TestEnv @{
    HOME = $homeDir
    USERPROFILE = $homeDir
    LOCALAPPDATA = $localAppData
    APPDATA = $appData
    XDG_CONFIG_HOME = $xdgConfigHome
    XDG_DATA_HOME = $xdgDataHome
    XDG_STATE_HOME = $xdgStateHome
    CODEX_REMOTE_REPO_ROOT = $repoRootSentinel
    CODEX_REMOTE_CONFIG = $null
    CODEX_REMOTE_INSTANCE_ID = $null
    CODEX_REMOTE_VERSION = $Version
    CODEX_REMOTE_BASE_URL = ("http://127.0.0.1:{0}" -f $port)
    CODEX_REMOTE_INSTALL_ROOT = $installRoot
    CODEX_REMOTE_RELEASES_API_URL = $null
    CODEX_REMOTE_SKIP_SETUP = $null
  }

  & (Join-Path $RootDir "install-release.ps1") -InstallArgs @("-base-dir", $homeDir)
  if ($LASTEXITCODE -ne 0) {
    throw "production install-release.ps1 failed with exit code $LASTEXITCODE."
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
  if ([string]$configPayload.admin.listenPort -ne [string]$adminPort) {
    throw "admin.listenPort=$($configPayload.admin.listenPort) want $adminPort"
  }
  if ([string]$configPayload.relay.listenPort -ne [string]$relayPort) {
    throw "relay.listenPort=$($configPayload.relay.listenPort) want $relayPort"
  }

  & $binaryPath version | Out-Null
  if ($LASTEXITCODE -ne 0) {
    throw "installed production binary version check failed with exit code $LASTEXITCODE."
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
    throw "beta install-release.ps1 failed with exit code $LASTEXITCODE."
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
    throw "installed beta binary version check failed with exit code $LASTEXITCODE."
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
