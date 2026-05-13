Unicode True
RequestExecutionLevel user
ShowInstDetails show
XPStyle on
SetCompressor /SOLID lzma

!include "LogicLib.nsh"

!ifndef APP_VERSION
!error "APP_VERSION define is required"
!endif
!ifndef RELEASE_TRACK
!error "RELEASE_TRACK define is required"
!endif
!ifndef PAYLOAD_BINARY
!error "PAYLOAD_BINARY define is required"
!endif
!ifndef OUTPUT_FILE
!error "OUTPUT_FILE define is required"
!endif

Name "Codex Remote Feishu ${APP_VERSION}"
OutFile "${OUTPUT_FILE}"
Caption "Codex Remote Feishu Installer"
BrandingText "Codex Remote Feishu"

Var ResultFile
Var ExitCode
Var OKValue
Var SetupRequired
Var SetupURL
Var AdminURL
Var LogPath
Var ErrorText
Var SuccessText

Page instfiles

Section "Install"
  InitPluginsDir
  SetOutPath "$PLUGINSDIR\payload"
  File /oname=codex-remote.exe "${PAYLOAD_BINARY}"

  StrCpy $ResultFile "$PLUGINSDIR\packaged-install-result.ini"

  DetailPrint "Running packaged installer bridge..."
  nsExec::ExecToLog '"$PLUGINSDIR\payload\codex-remote.exe" packaged-install -binary "$PLUGINSDIR\payload\codex-remote.exe" -install-source release -current-version "${APP_VERSION}" -current-track "${RELEASE_TRACK}" -service-manager task_scheduler_logon -format text -result-file "$ResultFile"'
  Pop $ExitCode

  ReadINIStr $OKValue "$ResultFile" "result" "ok"
  ReadINIStr $SetupRequired "$ResultFile" "result" "setupRequired"
  ReadINIStr $SetupURL "$ResultFile" "result" "setupURL"
  ReadINIStr $AdminURL "$ResultFile" "result" "adminURL"
  ReadINIStr $LogPath "$ResultFile" "result" "logPath"
  ReadINIStr $ErrorText "$ResultFile" "result" "error"

  ${If} $ExitCode != "0"
  ${OrIf} $OKValue != "true"
    ${If} $ErrorText == ""
      StrCpy $ErrorText "The packaged-install bridge failed."
    ${EndIf}
    ${If} $LogPath != ""
      MessageBox MB_ICONSTOP|MB_OK "$ErrorText$\r$\n$\r$\nLogs: $LogPath"
    ${Else}
      MessageBox MB_ICONSTOP|MB_OK "$ErrorText"
    ${EndIf}
    SetErrorLevel 1
    Quit
  ${EndIf}

  ${If} $SetupRequired == "true"
    ${If} $SetupURL != ""
      DetailPrint "Opening WebSetup: $SetupURL"
      ExecShell "open" "$SetupURL"
      StrCpy $SuccessText "Installation completed.$\r$\n$\r$\nWebSetup is opening in your browser.$\r$\nIf it does not open automatically, use:$\r$\n$SetupURL"
    ${ElseIf} $AdminURL != ""
      StrCpy $SuccessText "Installation completed.$\r$\n$\r$\nWebSetup is required. Open:$\r$\n$AdminURL"
    ${Else}
      StrCpy $SuccessText "Installation completed."
    ${EndIf}
  ${ElseIf} $AdminURL != ""
    StrCpy $SuccessText "Installation completed.$\r$\n$\r$\nAdmin UI: $AdminURL"
  ${Else}
    StrCpy $SuccessText "Installation completed."
  ${EndIf}

  ${If} $LogPath != ""
    StrCpy $SuccessText "$SuccessText$\r$\n$\r$\nLogs: $LogPath"
  ${EndIf}

  MessageBox MB_OK "$SuccessText"
SectionEnd
