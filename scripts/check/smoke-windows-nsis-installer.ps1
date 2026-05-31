param(
  [string]$ProdInstallerPath = "",
  [string]$BetaInstallerPath = "",
  [switch]$Help
)

$ErrorActionPreference = "Stop"

function Show-Usage {
  @'
usage: scripts/check/smoke-windows-nsis-installer.ps1 [options]

options:
  -ProdInstallerPath <path>  production NSIS installer to smoke
  -BetaInstallerPath <path>  optional beta NSIS installer to smoke for build sanity
  -Help                      show this help
'@ | Write-Output
}

function Require-File([string]$PathValue, [string]$Label) {
  if ([string]::IsNullOrWhiteSpace($PathValue)) {
    throw "$Label is required."
  }
  if (-not (Test-Path -LiteralPath $PathValue -PathType Leaf)) {
    throw "$Label not found: $PathValue"
  }
}

function New-TestDirectory {
  $path = Join-Path ([System.IO.Path]::GetTempPath()) ("codex-remote-nsis-smoke-" + [Guid]::NewGuid().ToString("N"))
  New-Item -ItemType Directory -Force -Path $path | Out-Null
  return $path
}

function Write-IniFile([string]$PathValue, [hashtable]$Values) {
  $lines = @("[result]")
  foreach ($key in $Values.Keys) {
    $value = [string]$Values[$key]
    $safeValue = $value.Replace("`r", " ").Replace("`n", " ")
    $lines += ("{0}={1}" -f $key, $safeValue)
  }
  Set-Content -LiteralPath $PathValue -Value $lines -Encoding UTF8
}

function Read-CaptureFile([string]$PathValue) {
  $result = @{}
  foreach ($rawLine in Get-Content -LiteralPath $PathValue -Encoding Unicode) {
    if ([string]::IsNullOrWhiteSpace($rawLine)) {
      continue
    }
    $parts = $rawLine -split "=", 2
    if ($parts.Length -ne 2) {
      continue
    }
    $result[$parts[0]] = $parts[1]
  }
  return $result
}

function Assert-Equal([string]$Name, $Actual, $Expected) {
  if ([string]$Actual -cne [string]$Expected) {
    throw "$Name mismatch. actual=[$Actual] expected=[$Expected]"
  }
}

function Assert-Contains([string]$Name, [string]$Actual, [string]$ExpectedFragment) {
  if ([string]::IsNullOrEmpty($Actual) -or -not $Actual.Contains($ExpectedFragment)) {
    throw "$Name mismatch. actual=[$Actual] expected to contain=[$ExpectedFragment]"
  }
}

function Invoke-InstallerScenario(
  [string]$InstallerPath,
  [string]$LanguageId,
  [hashtable]$ProbeValues,
  [hashtable]$ResultValues,
  [switch]$AutoPrimary
) {
  $tempDir = New-TestDirectory
  $probePath = Join-Path $tempDir "probe.ini"
  $resultPath = Join-Path $tempDir "result.ini"
  $capturePath = Join-Path $tempDir "capture.txt"
  try {
    Write-IniFile $probePath $ProbeValues
    Write-IniFile $resultPath $ResultValues

    $startInfo = New-Object System.Diagnostics.ProcessStartInfo
    $startInfo.FileName = $InstallerPath
    [void]$startInfo.ArgumentList.Add("/S")
    [void]$startInfo.ArgumentList.Add("/LANG=$LanguageId")
    $startInfo.UseShellExecute = $false
    $startInfo.WorkingDirectory = $tempDir
    $startInfo.Environment["CODEX_REMOTE_INSTALLER_TEST_CAPTURE_FILE"] = $capturePath
    $startInfo.Environment["CODEX_REMOTE_INSTALLER_TEST_PROBE_FILE"] = $probePath
    $startInfo.Environment["CODEX_REMOTE_INSTALLER_TEST_RESULT_FILE"] = $resultPath
    $startInfo.Environment["CODEX_REMOTE_INSTALLER_TEST_LANGUAGE"] = $LanguageId
    if ($AutoPrimary) {
      $startInfo.Environment["CODEX_REMOTE_INSTALLER_TEST_AUTO_PRIMARY"] = "1"
    }

    $process = [System.Diagnostics.Process]::Start($startInfo)
    if ($null -eq $process) {
      throw "failed to launch installer: $InstallerPath"
    }
    $process.WaitForExit()
    if (-not (Test-Path -LiteralPath $capturePath -PathType Leaf)) {
      throw "installer did not produce capture file: $capturePath"
    }
    return @{
      ExitCode = $process.ExitCode
      Capture = Read-CaptureFile $capturePath
    }
  } finally {
    if (Test-Path -LiteralPath $tempDir) {
      Remove-Item -LiteralPath $tempDir -Force -Recurse -ErrorAction SilentlyContinue
    }
  }
}

function Assert-NoKey([hashtable]$Capture, [string]$Key) {
  if ($Capture.ContainsKey($Key)) {
    throw "unexpected capture key present: $Key=$($Capture[$Key])"
  }
}

function Test-EnglishFreshInstallSetup([string]$InstallerPath) {
  $setupUrl = "http://127.0.0.1:43123/setup"
  $result = Invoke-InstallerScenario -InstallerPath $InstallerPath -LanguageId "1033" -AutoPrimary `
    -ProbeValues @{
      ok = "true"
      mode = "first_install"
      sameVersion = "false"
      startupMode = "login_autostart"
    } `
    -ResultValues @{
      ok = "true"
      mode = "first_install"
      setupRequired = "true"
      setupURL = $setupUrl
      adminURL = "http://127.0.0.1:43123/"
      logPath = "C:\temp\codex-remote.log"
      currentVersion = "v0.0.0"
      currentTrack = "production"
      startupMode = "login_autostart"
    }

  $capture = $result.Capture
  Assert-Equal "fresh-setup failure flag" $capture["resultFailure"] "false"
  Assert-Equal "fresh-setup action" $capture["primaryActionKind"] "continue_websetup"
  Assert-Equal "fresh-setup button" $capture["primaryButtonText"] "Continue WebSetup"
  Assert-Equal "fresh-setup title" $capture["titleText"] "Base installation completed"
  Assert-Contains "fresh-setup body" $capture["bodyText"] "Continue with WebSetup"
  Assert-Equal "fresh-setup admin hidden" $capture["showAdminLink"] "false"
  Assert-Equal "fresh-setup logs shown" $capture["showLogsLink"] "true"
  Assert-Equal "fresh-setup opened target" $capture["openedTarget"] $setupUrl
  Assert-Equal "fresh-setup log link text" $capture["logsLinkText"] "Open Logs"
}

function Test-ChineseFreshInstallComplete([string]$InstallerPath) {
  $result = Invoke-InstallerScenario -InstallerPath $InstallerPath -LanguageId "2052" `
    -ProbeValues @{
      ok = "true"
      mode = "first_install"
      sameVersion = "false"
      startupMode = "manual"
    } `
    -ResultValues @{
      ok = "true"
      mode = "first_install"
      setupRequired = "false"
      setupURL = ""
      adminURL = "http://127.0.0.1:43123/"
      logPath = "C:\temp\codex-remote.log"
      currentVersion = "v0.0.0"
      currentTrack = "production"
      startupMode = "manual"
    }

  $capture = $result.Capture
  Assert-Equal "zh fresh-complete failure flag" $capture["resultFailure"] "false"
  Assert-Equal "zh fresh-complete action" $capture["primaryActionKind"] "finish"
  Assert-Equal "zh fresh-complete button" $capture["primaryButtonText"] "完成"
  Assert-Equal "zh fresh-complete title" $capture["titleText"] "安装完成"
  Assert-Contains "zh fresh-complete body" $capture["bodyText"] "Codex Remote 已安装完成"
  Assert-Equal "zh fresh-complete admin shown" $capture["showAdminLink"] "true"
  Assert-Equal "zh fresh-complete logs shown" $capture["showLogsLink"] "true"
  Assert-Equal "zh admin link text" $capture["adminLinkText"] "打开 Admin UI"
  Assert-Equal "zh log link text" $capture["logsLinkText"] "打开日志"
  Assert-NoKey $capture "openedTarget"
}

function Test-EnglishRepair([string]$InstallerPath) {
  $result = Invoke-InstallerScenario -InstallerPath $InstallerPath -LanguageId "1033" `
    -ProbeValues @{
      ok = "true"
      mode = "repair"
      sameVersion = "true"
      currentVersion = "v0.0.0"
      startupMode = "login_autostart"
    } `
    -ResultValues @{
      ok = "true"
      mode = "repair"
      setupRequired = "false"
      setupURL = "http://127.0.0.1:43123/setup"
      adminURL = "http://127.0.0.1:43123/"
      logPath = "C:\temp\codex-remote.log"
      currentVersion = "v0.0.0"
      currentTrack = "production"
      startupMode = "login_autostart"
    }

  $capture = $result.Capture
  Assert-Equal "repair failure flag" $capture["resultFailure"] "false"
  Assert-Equal "repair action" $capture["primaryActionKind"] "finish"
  Assert-Equal "repair title" $capture["titleText"] "Reinstallation completed"
  Assert-Contains "repair body" $capture["bodyText"] "reinstalled successfully"
  Assert-Equal "repair admin shown" $capture["showAdminLink"] "true"
  Assert-Equal "repair logs shown" $capture["showLogsLink"] "true"
  Assert-NoKey $capture "openedTarget"
}

function Test-EnglishFailure([string]$InstallerPath) {
  $result = Invoke-InstallerScenario -InstallerPath $InstallerPath -LanguageId "1033" `
    -ProbeValues @{
      ok = "true"
      mode = "first_install"
      sameVersion = "false"
      startupMode = "manual"
    } `
    -ResultValues @{
      ok = "false"
      mode = "first_install"
      setupRequired = "false"
      setupURL = ""
      adminURL = "http://127.0.0.1:43123/"
      logPath = "C:\temp\codex-remote.log"
      error = "simulated install failure"
    }

  $capture = $result.Capture
  Assert-Equal "failure flag" $capture["resultFailure"] "true"
  Assert-Equal "failure action" $capture["primaryActionKind"] "finish"
  Assert-Equal "failure title" $capture["titleText"] "Installation failed"
  Assert-Contains "failure body" $capture["bodyText"] "simulated install failure"
  Assert-Equal "failure admin hidden" $capture["showAdminLink"] "false"
  Assert-Equal "failure logs shown" $capture["showLogsLink"] "true"
  Assert-NoKey $capture "openedTarget"
}

function Test-BetaBuildLaunches([string]$InstallerPath) {
  $result = Invoke-InstallerScenario -InstallerPath $InstallerPath -LanguageId "1033" `
    -ProbeValues @{
      ok = "true"
      mode = "first_install"
      sameVersion = "false"
      startupMode = "manual"
    } `
    -ResultValues @{
      ok = "true"
      mode = "first_install"
      setupRequired = "false"
      setupURL = ""
      adminURL = "http://127.0.0.1:43123/"
      logPath = "C:\temp\codex-remote.log"
      currentVersion = "v0.1.0-beta.1"
      currentTrack = "beta"
      startupMode = "manual"
    }

  $capture = $result.Capture
  Assert-Equal "beta build failure flag" $capture["resultFailure"] "false"
  Assert-Equal "beta build title" $capture["titleText"] "Installation completed"
}

if ($Help) {
  Show-Usage
  exit 0
}

Require-File $ProdInstallerPath "ProdInstallerPath"
Test-EnglishFreshInstallSetup $ProdInstallerPath
Test-ChineseFreshInstallComplete $ProdInstallerPath
Test-EnglishRepair $ProdInstallerPath
Test-EnglishFailure $ProdInstallerPath

if (-not [string]::IsNullOrWhiteSpace($BetaInstallerPath)) {
  Require-File $BetaInstallerPath "BetaInstallerPath"
  Test-BetaBuildLaunches $BetaInstallerPath
}

Write-Output "Windows NSIS installer smoke passed."
