param(
  [string]$Version = $env:CODEX_REMOTE_VERSION,
  [string]$Track = "production",
  [string]$Repo = $(if ($env:CODEX_REMOTE_REPO) { $env:CODEX_REMOTE_REPO } else { "kxn/codex-remote-feishu" }),
  [string]$InstallRoot = $env:CODEX_REMOTE_INSTALL_ROOT,
  [string]$BaseUrl = $env:CODEX_REMOTE_BASE_URL,
  [string]$ReleasesApiUrl = $env:CODEX_REMOTE_RELEASES_API_URL,
  [switch]$DownloadOnly,
  [switch]$Help,
  [string[]]$InstallArgs = @()
)

$ErrorActionPreference = "Stop"

function Show-Usage {
  @'
Usage: install-release.ps1 [options] [-InstallArgs <args...>]

Downloads the latest compatible Codex Remote Feishu production release package,
extracts it locally, bootstraps the installed binary, starts the local
daemon, and prints the WebSetup URL.

Options:
  -Version <version>      Install a specific version instead of track latest
  -Track <name>           Install the latest release from production|beta|alpha
  -Repo <owner/name>      GitHub repository to use
  -InstallRoot <dir>      Directory used to store downloaded releases
  -DownloadOnly           Download and extract, but do not run codex-remote install
  -Help                   Show this help

Environment overrides:
  CODEX_REMOTE_VERSION
  CODEX_REMOTE_REPO
  CODEX_REMOTE_BASE_URL
  CODEX_REMOTE_INSTALL_ROOT
  CODEX_REMOTE_SKIP_SETUP=1

Examples:
  irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1 | iex
  & ([scriptblock]::Create((irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1))) -Track beta
  & ([scriptblock]::Create((irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1))) -Version v1.0.0
  & ([scriptblock]::Create((irm https://raw.githubusercontent.com/kxn/codex-remote-feishu/master/install-release.ps1))) -InstallArgs '-instance','beta'
'@ | Write-Output
}

function Test-Truthy([string]$Value) {
  $normalized = ""
  if ($null -ne $Value) {
    $normalized = $Value.Trim().ToLowerInvariant()
  }
  switch ($normalized) {
    "1" { return $true }
    "true" { return $true }
    "yes" { return $true }
    "on" { return $true }
    default { return $false }
  }
}

function Assert-Track([string]$Value) {
  $normalized = ""
  if ($null -ne $Value) {
    $normalized = $Value.Trim().ToLowerInvariant()
  }
  switch ($normalized) {
    "production" { return "production" }
    "beta" { return "beta" }
    "alpha" { return "alpha" }
    default {
      throw "Unsupported release track: $Value. Use production, beta, or alpha."
    }
  }
}

function Get-LocalAppDataRoot {
  foreach ($candidate in @(
    $env:LOCALAPPDATA,
    [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData),
    $(if ($env:USERPROFILE) { Join-Path $env:USERPROFILE "AppData\Local" } else { $null })
  )) {
    if (-not [string]::IsNullOrWhiteSpace($candidate)) {
      return $candidate.Trim()
    }
  }
  throw "Unable to determine LOCALAPPDATA for the Windows installer."
}

function Get-DefaultInstallRoot {
  return Join-Path (Join-Path (Get-LocalAppDataRoot) "codex-remote") "releases"
}

function Get-GoArch {
  foreach ($candidate in @(
    $env:PROCESSOR_ARCHITEW6432,
    $env:PROCESSOR_ARCHITECTURE,
    [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
  )) {
    $normalized = ""
    if ($null -ne $candidate) {
      $normalized = $candidate.Trim().ToUpperInvariant()
    }
    switch ($normalized) {
      "AMD64" { return "amd64" }
      "X64" { return "amd64" }
      "X86_64" { return "amd64" }
    }
  }
  throw "Unsupported Windows architecture. The online installer currently supports x64 only."
}

function Ensure-SystemNetHttp {
  if ("System.Net.Http.HttpClientHandler" -as [type]) {
    return
  }

  # Windows PowerShell 5 does not always preload System.Net.Http for scriptblocks fetched via irm/iex.
  try {
    Add-Type -AssemblyName System.Net.Http | Out-Null
  } catch {
    throw "Failed to load System.Net.Http for the Windows installer. $($_.Exception.Message)"
  }

  if (-not ("System.Net.Http.HttpClientHandler" -as [type])) {
    throw "System.Net.Http is unavailable in this PowerShell session."
  }
}

function Add-AuthHeader([System.Net.Http.HttpRequestMessage]$Request) {
  $token = ""
  foreach ($candidate in @($env:CODEX_REMOTE_GITHUB_TOKEN, $env:GITHUB_TOKEN, $env:GH_TOKEN)) {
    if (-not [string]::IsNullOrWhiteSpace($candidate)) {
      $token = $candidate.Trim()
      break
    }
  }
  if (-not [string]::IsNullOrWhiteSpace($token)) {
    $Request.Headers.Authorization = New-Object System.Net.Http.Headers.AuthenticationHeaderValue -ArgumentList "Bearer", $token
  }
}

function Invoke-HttpRequest([string]$Url) {
  Ensure-SystemNetHttp
  $handler = New-Object System.Net.Http.HttpClientHandler
  if ($Url -match '^https?://(127\.0\.0\.1|localhost)(:\d+)?(/|$)') {
    $handler.UseProxy = $false
  }
  $client = New-Object System.Net.Http.HttpClient -ArgumentList $handler
  $client.Timeout = [TimeSpan]::FromMinutes(10)
  $client.DefaultRequestHeaders.UserAgent.ParseAdd("codex-remote-feishu-install")

  $request = New-Object System.Net.Http.HttpRequestMessage -ArgumentList ([System.Net.Http.HttpMethod]::Get), $Url
  if ($Url -like "https://api.github.com/*") {
    $request.Headers.Accept.ParseAdd("application/vnd.github+json")
  }
  Add-AuthHeader $request

  $response = $client.SendAsync($request).GetAwaiter().GetResult()
  [void]$response.EnsureSuccessStatusCode()
  return $response
}

function Invoke-TextRequest([string]$Url) {
  $response = Invoke-HttpRequest $Url
  try {
    return $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
  } finally {
    $response.Dispose()
  }
}

function Invoke-DownloadRequest([string]$Url, [string]$OutFile) {
  $response = Invoke-HttpRequest $Url
  try {
    $directory = Split-Path -Parent $OutFile
    if (-not [string]::IsNullOrWhiteSpace($directory)) {
      New-Item -ItemType Directory -Force -Path $directory | Out-Null
    }
    $stream = $response.Content.ReadAsStreamAsync().GetAwaiter().GetResult()
    try {
      $file = [System.IO.File]::Open($OutFile, [System.IO.FileMode]::Create, [System.IO.FileAccess]::Write, [System.IO.FileShare]::None)
      try {
        ([System.IO.Stream]$stream).CopyTo([System.IO.Stream]$file)
      } finally {
        $file.Dispose()
      }
    } finally {
      $stream.Dispose()
    }
  } finally {
    $response.Dispose()
  }
}

function Normalize-Version([string]$Value) {
  if ([string]::IsNullOrWhiteSpace($Value)) {
    return ""
  }
  $trimmed = $Value.Trim()
  if ($trimmed.StartsWith("v")) {
    return $trimmed
  }
  return "v$trimmed"
}

function Resolve-LatestVersionFromReleaseApi([string]$SelectedTrack) {
  $apiUrl = $ReleasesApiUrl
  if ([string]::IsNullOrWhiteSpace($apiUrl)) {
    $apiUrl = "https://api.github.com/repos/$Repo/releases?per_page=100"
  }
  $releasePayload = Invoke-TextRequest $apiUrl | ConvertFrom-Json
  if ($releasePayload -is [System.Array]) {
    $releases = $releasePayload
  } elseif ($null -eq $releasePayload) {
    $releases = @()
  } else {
    $releases = @($releasePayload)
  }
  switch ($SelectedTrack) {
    "production" {
      $tagPattern = '^v[0-9]+\.[0-9]+\.[0-9]+$'
      $expectedPrerelease = $false
    }
    "beta" {
      $tagPattern = '^v[0-9]+\.[0-9]+\.[0-9]+-beta\.[0-9]+$'
      $expectedPrerelease = $true
    }
    "alpha" {
      $tagPattern = '^v[0-9]+\.[0-9]+\.[0-9]+-alpha\.[0-9]+$'
      $expectedPrerelease = $true
    }
    default {
      throw "Unsupported release track: $SelectedTrack"
    }
  }

  foreach ($release in $releases) {
    if ($null -eq $release) {
      continue
    }
    if ($release.draft -ne $false) {
      continue
    }
    if ([bool]$release.prerelease -ne $expectedPrerelease) {
      continue
    }
    $tagName = [string]$release.tag_name
    if ($tagName -match $tagPattern) {
      return $tagName
    }
  }

  throw "Failed to resolve latest $SelectedTrack release."
}

function Resolve-LatestVersion([string]$SelectedTrack) {
  if ($SelectedTrack -ne "production" -or -not [string]::IsNullOrWhiteSpace($ReleasesApiUrl)) {
    return Resolve-LatestVersionFromReleaseApi $SelectedTrack
  }

  $latestApiUrl = "https://api.github.com/repos/$Repo/releases/latest"
  $latest = Invoke-TextRequest $latestApiUrl | ConvertFrom-Json
  if ($null -eq $latest -or [string]::IsNullOrWhiteSpace([string]$latest.tag_name)) {
    throw "Failed to resolve latest production release."
  }
  return [string]$latest.tag_name
}

function Update-CurrentLink([string]$InstallRootPath, [string]$TargetDir) {
  $currentPath = Join-Path $InstallRootPath "current"
  if (Test-Path -LiteralPath $currentPath) {
    Remove-Item -LiteralPath $currentPath -Force -Recurse
  }
  New-Item -ItemType Junction -Path $currentPath -Target $TargetDir | Out-Null
}

if ($Help) {
  Show-Usage
  return
}

$Track = Assert-Track $Track
$skipSetup = Test-Truthy $env:CODEX_REMOTE_SKIP_SETUP
$goarch = Get-GoArch
if ([string]::IsNullOrWhiteSpace($InstallRoot)) {
  $InstallRoot = Get-DefaultInstallRoot
}
if ([string]::IsNullOrWhiteSpace($Version)) {
  $Version = Resolve-LatestVersion $Track
}
$Version = Normalize-Version $Version
New-Item -ItemType Directory -Force -Path $InstallRoot | Out-Null

$assetName = "codex-remote-feishu_{0}_windows_{1}.zip" -f $Version.TrimStart("v"), $goarch
if ([string]::IsNullOrWhiteSpace($BaseUrl)) {
  $assetUrl = "https://github.com/$Repo/releases/download/$Version/$assetName"
} else {
  $assetUrl = "{0}/{1}" -f $BaseUrl.TrimEnd("/"), $assetName
}

$tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("codex-remote-install-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null
try {
  $archivePath = Join-Path $tmpDir $assetName
  Invoke-DownloadRequest $assetUrl $archivePath
  Expand-Archive -Path $archivePath -DestinationPath $tmpDir -Force

  $packageDir = Join-Path $tmpDir ("codex-remote-feishu_{0}_windows_{1}" -f $Version.TrimStart("v"), $goarch)
  if (-not (Test-Path -LiteralPath $packageDir -PathType Container)) {
    throw "Downloaded archive did not contain the expected package directory."
  }

  $targetDir = Join-Path $InstallRoot $Version
  if (Test-Path -LiteralPath $targetDir) {
    Remove-Item -LiteralPath $targetDir -Force -Recurse
  }
  Move-Item -LiteralPath $packageDir -Destination $targetDir
  Update-CurrentLink $InstallRoot $targetDir

  Write-Output "Downloaded $Version to $targetDir"
  Write-Output "Current release link: $(Join-Path $InstallRoot 'current')"

  if ($DownloadOnly.IsPresent -or $skipSetup) {
    return
  }

  $binaryPath = Join-Path $targetDir "codex-remote.exe"
  if (-not (Test-Path -LiteralPath $binaryPath -PathType Leaf)) {
    throw "Downloaded package did not contain an executable codex-remote binary."
  }

  & $binaryPath install `
    -binary $binaryPath `
    -install-source release `
    -current-track $Track `
    -current-version $Version `
    -versions-root $InstallRoot `
    -current-slot $Version `
    -bootstrap-only `
    -start-daemon `
    @InstallArgs
  if ($LASTEXITCODE -ne 0) {
    throw "codex-remote install failed with exit code $LASTEXITCODE."
  }
} finally {
  if (Test-Path -LiteralPath $tmpDir) {
    Remove-Item -LiteralPath $tmpDir -Force -Recurse
  }
}
