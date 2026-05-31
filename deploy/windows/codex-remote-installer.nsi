Unicode True
RequestExecutionLevel user
ShowInstDetails show
SetCompressor /SOLID lzma

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "WinMessages.nsh"
!include "nsDialogs.nsh"

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

!define LANGID_ENGLISH 1033
!define LANGID_SIMPCHINESE 2052

Name "Codex Remote Feishu"
OutFile "${OUTPUT_FILE}"
Caption "$(STR_CAPTION)"
BrandingText "$(STR_BRANDING)"

!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_NOREBOOTSUPPORT
!define MUI_FINISHPAGE_TEXT " "
!define MUI_FINISHPAGE_BUTTON "$(STR_BUTTON_FINISH)"
!define MUI_PAGE_CUSTOMFUNCTION_SHOW FinishPageShow
!define MUI_PAGE_CUSTOMFUNCTION_LEAVE FinishPageLeave

Var PayloadBinary
Var ProbeFile
Var ResultFile
Var ProbeExitCode
Var ExitCode
Var ProbeOKValue
Var ProbeMode
Var ProbeSameVersion
Var ProbeCurrentVersion
Var ProbeStartupMode
Var OKValue
Var ResultMode
Var SetupRequired
Var SetupURL
Var AdminURL
Var LogPath
Var ErrorText
Var CurrentVersion
Var CurrentTrack
Var CurrentSlot
Var StartupMode
Var ResultTitleText
Var ResultBodyText
Var ResultFailure
Var PrimaryActionKind
Var PrimaryActionTarget
Var PrimaryButtonText
Var ShowAdminLink
Var ShowLogsLink
Var FinishAdminLink
Var FinishLogsLink
Var TestCaptureFile
Var TestProbeOverrideFile
Var TestResultOverrideFile
Var TestAutoPrimary
Var TestLanguage

LangString STR_CAPTION ${LANGID_ENGLISH} "Codex Remote Feishu Installer"
LangString STR_CAPTION ${LANGID_SIMPCHINESE} "Codex Remote Feishu 安装器"
LangString STR_BRANDING ${LANGID_ENGLISH} "Codex Remote Feishu"
LangString STR_BRANDING ${LANGID_SIMPCHINESE} "Codex Remote Feishu"

LangString STR_FINISH_TITLE_INSTALL_SETUP ${LANGID_ENGLISH} "Base installation completed"
LangString STR_FINISH_TITLE_INSTALL_SETUP ${LANGID_SIMPCHINESE} "基础安装已完成"
LangString STR_FINISH_TITLE_INSTALL_COMPLETE ${LANGID_ENGLISH} "Installation completed"
LangString STR_FINISH_TITLE_INSTALL_COMPLETE ${LANGID_SIMPCHINESE} "安装完成"
LangString STR_FINISH_TITLE_REINSTALL_COMPLETE ${LANGID_ENGLISH} "Reinstallation completed"
LangString STR_FINISH_TITLE_REINSTALL_COMPLETE ${LANGID_SIMPCHINESE} "重装修复已完成"
LangString STR_FINISH_TITLE_UPGRADE_COMPLETE ${LANGID_ENGLISH} "Upgrade completed"
LangString STR_FINISH_TITLE_UPGRADE_COMPLETE ${LANGID_SIMPCHINESE} "升级完成"
LangString STR_FINISH_TITLE_REPAIR_COMPLETE ${LANGID_ENGLISH} "Repair completed"
LangString STR_FINISH_TITLE_REPAIR_COMPLETE ${LANGID_SIMPCHINESE} "修复完成"
LangString STR_FINISH_TITLE_FAILURE ${LANGID_ENGLISH} "Installation failed"
LangString STR_FINISH_TITLE_FAILURE ${LANGID_SIMPCHINESE} "安装失败"

LangString STR_FINISH_SUMMARY_INSTALL_SETUP ${LANGID_ENGLISH} "Codex Remote is installed and the background service is ready. Continue with WebSetup to finish the initial configuration."
LangString STR_FINISH_SUMMARY_INSTALL_SETUP ${LANGID_SIMPCHINESE} "Codex Remote 已完成基础安装，后台服务已就绪。继续进入 WebSetup 以完成首次配置。"
LangString STR_FINISH_SUMMARY_INSTALL_COMPLETE ${LANGID_ENGLISH} "Codex Remote is installed and ready to use."
LangString STR_FINISH_SUMMARY_INSTALL_COMPLETE ${LANGID_SIMPCHINESE} "Codex Remote 已安装完成，可以开始使用。"
LangString STR_FINISH_SUMMARY_REINSTALL_COMPLETE ${LANGID_ENGLISH} "The current installation was reinstalled successfully."
LangString STR_FINISH_SUMMARY_REINSTALL_COMPLETE ${LANGID_SIMPCHINESE} "当前安装已成功重装修复。"
LangString STR_FINISH_SUMMARY_UPGRADE_COMPLETE ${LANGID_ENGLISH} "The current installation was upgraded successfully."
LangString STR_FINISH_SUMMARY_UPGRADE_COMPLETE ${LANGID_SIMPCHINESE} "当前安装已成功升级。"
LangString STR_FINISH_SUMMARY_REPAIR_COMPLETE ${LANGID_ENGLISH} "The current installation was repaired successfully."
LangString STR_FINISH_SUMMARY_REPAIR_COMPLETE ${LANGID_SIMPCHINESE} "当前安装已成功修复。"
LangString STR_FINISH_SUMMARY_FAILURE_DEFAULT ${LANGID_ENGLISH} "The installer could not complete the operation."
LangString STR_FINISH_SUMMARY_FAILURE_DEFAULT ${LANGID_SIMPCHINESE} "安装器未能完成本次操作。"
LangString STR_FINISH_SUMMARY_FAILURE_FOOTER ${LANGID_ENGLISH} "Open logs to inspect the daemon output and try again."
LangString STR_FINISH_SUMMARY_FAILURE_FOOTER ${LANGID_SIMPCHINESE} "请打开日志查看 daemon 输出后重试。"

LangString STR_DETAIL_VERSION ${LANGID_ENGLISH} "Installed version: "
LangString STR_DETAIL_VERSION ${LANGID_SIMPCHINESE} "已安装版本："
LangString STR_DETAIL_STARTUP_MODE ${LANGID_ENGLISH} "Startup mode: "
LangString STR_DETAIL_STARTUP_MODE ${LANGID_SIMPCHINESE} "启动方式："
LangString STR_STARTUP_MANUAL ${LANGID_ENGLISH} "Manual / on-demand"
LangString STR_STARTUP_MANUAL ${LANGID_SIMPCHINESE} "手动 / 按需启动"
LangString STR_STARTUP_LOGIN_AUTOSTART ${LANGID_ENGLISH} "Start automatically after sign-in"
LangString STR_STARTUP_LOGIN_AUTOSTART ${LANGID_SIMPCHINESE} "登录后自动启动"

LangString STR_LINK_OPEN_ADMIN ${LANGID_ENGLISH} "Open Admin UI"
LangString STR_LINK_OPEN_ADMIN ${LANGID_SIMPCHINESE} "打开 Admin UI"
LangString STR_LINK_OPEN_LOGS ${LANGID_ENGLISH} "Open Logs"
LangString STR_LINK_OPEN_LOGS ${LANGID_SIMPCHINESE} "打开日志"

LangString STR_BUTTON_CONTINUE_WEBSETUP ${LANGID_ENGLISH} "Continue WebSetup"
LangString STR_BUTTON_CONTINUE_WEBSETUP ${LANGID_SIMPCHINESE} "继续 WebSetup"
LangString STR_BUTTON_FINISH ${LANGID_ENGLISH} "Finish"
LangString STR_BUTTON_FINISH ${LANGID_SIMPCHINESE} "完成"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "SimpChinese"

Function .onInit
  ReadEnvStr $TestCaptureFile "CODEX_REMOTE_INSTALLER_TEST_CAPTURE_FILE"
  ReadEnvStr $TestProbeOverrideFile "CODEX_REMOTE_INSTALLER_TEST_PROBE_FILE"
  ReadEnvStr $TestResultOverrideFile "CODEX_REMOTE_INSTALLER_TEST_RESULT_FILE"
  ReadEnvStr $TestAutoPrimary "CODEX_REMOTE_INSTALLER_TEST_AUTO_PRIMARY"
  ReadEnvStr $TestLanguage "CODEX_REMOTE_INSTALLER_TEST_LANGUAGE"
  ${If} $TestLanguage != ""
    StrCpy $LANGUAGE $TestLanguage
  ${EndIf}
  ${If} $TestCaptureFile != ""
    SetSilent silent
  ${EndIf}
FunctionEnd

Section "Install"
  Call ResetDerivedState

  InitPluginsDir
  SetOutPath "$PLUGINSDIR\payload"
  File /oname=codex-remote.exe "${PAYLOAD_BINARY}"
  StrCpy $PayloadBinary "$PLUGINSDIR\payload\codex-remote.exe"
  StrCpy $ProbeFile "$PLUGINSDIR\packaged-install-probe.ini"
  StrCpy $ResultFile "$PLUGINSDIR\packaged-install-result.ini"

  ${If} $TestProbeOverrideFile != ""
    StrCpy $ProbeFile "$TestProbeOverrideFile"
    DetailPrint "Using test probe override: $ProbeFile"
    StrCpy $ProbeExitCode "0"
  ${Else}
    DetailPrint "Inspecting current installation..."
    nsExec::ExecToLog '"$PayloadBinary" packaged-install-probe -current-version "${APP_VERSION}" -format text -result-file "$ProbeFile"'
    Pop $ProbeExitCode
  ${EndIf}

  Call ReadProbeFile
  ${If} $ProbeExitCode != "0"
  ${OrIf} $ProbeOKValue != "true"
    ${If} $ErrorText == ""
      StrCpy $ErrorText "The packaged-install probe failed."
    ${EndIf}
    StrCpy $ResultFailure "true"
    Goto derive_result
  ${EndIf}

  ${If} $TestResultOverrideFile != ""
    StrCpy $ResultFile "$TestResultOverrideFile"
    DetailPrint "Using test install result override: $ResultFile"
    StrCpy $ExitCode "0"
  ${Else}
    DetailPrint "Running packaged installer bridge..."
    nsExec::ExecToLog '"$PayloadBinary" packaged-install -binary "$PayloadBinary" -install-source release -current-version "${APP_VERSION}" -current-track "${RELEASE_TRACK}" -format text -result-file "$ResultFile"'
    Pop $ExitCode
  ${EndIf}

  Call ReadResultFile
  ${If} $ExitCode != "0"
  ${OrIf} $OKValue != "true"
    ${If} $ErrorText == ""
      StrCpy $ErrorText "The packaged-install bridge failed."
    ${EndIf}
    StrCpy $ResultFailure "true"
  ${EndIf}

derive_result:
  Call DeriveResultState

  ${If} $TestCaptureFile != ""
    Call WriteTestCapture
    ${If} $TestAutoPrimary != ""
      Call PerformPrimaryAction
    ${EndIf}
    ${If} $ResultFailure == "true"
      SetErrorLevel 1
    ${EndIf}
    Quit
  ${EndIf}
SectionEnd

Function ResetDerivedState
  StrCpy $ProbeExitCode ""
  StrCpy $ExitCode ""
  StrCpy $ProbeOKValue ""
  StrCpy $ProbeMode ""
  StrCpy $ProbeSameVersion ""
  StrCpy $ProbeCurrentVersion ""
  StrCpy $ProbeStartupMode ""
  StrCpy $OKValue ""
  StrCpy $ResultMode ""
  StrCpy $SetupRequired ""
  StrCpy $SetupURL ""
  StrCpy $AdminURL ""
  StrCpy $LogPath ""
  StrCpy $ErrorText ""
  StrCpy $CurrentVersion ""
  StrCpy $CurrentTrack ""
  StrCpy $CurrentSlot ""
  StrCpy $StartupMode ""
  StrCpy $ResultTitleText ""
  StrCpy $ResultBodyText ""
  StrCpy $ResultFailure "false"
  StrCpy $PrimaryActionKind "finish"
  StrCpy $PrimaryActionTarget ""
  StrCpy $PrimaryButtonText "$(STR_BUTTON_FINISH)"
  StrCpy $ShowAdminLink "false"
  StrCpy $ShowLogsLink "false"
FunctionEnd

Function ReadProbeFile
  ReadINIStr $ProbeOKValue "$ProbeFile" "result" "ok"
  ReadINIStr $ProbeMode "$ProbeFile" "result" "mode"
  ReadINIStr $ProbeSameVersion "$ProbeFile" "result" "sameVersion"
  ReadINIStr $ProbeCurrentVersion "$ProbeFile" "result" "currentVersion"
  ReadINIStr $ProbeStartupMode "$ProbeFile" "result" "startupMode"
  ReadINIStr $ErrorText "$ProbeFile" "result" "error"
FunctionEnd

Function ReadResultFile
  ReadINIStr $OKValue "$ResultFile" "result" "ok"
  ReadINIStr $ResultMode "$ResultFile" "result" "mode"
  ReadINIStr $SetupRequired "$ResultFile" "result" "setupRequired"
  ReadINIStr $SetupURL "$ResultFile" "result" "setupURL"
  ReadINIStr $AdminURL "$ResultFile" "result" "adminURL"
  ReadINIStr $LogPath "$ResultFile" "result" "logPath"
  ReadINIStr $ErrorText "$ResultFile" "result" "error"
  ReadINIStr $CurrentVersion "$ResultFile" "result" "currentVersion"
  ReadINIStr $CurrentTrack "$ResultFile" "result" "currentTrack"
  ReadINIStr $CurrentSlot "$ResultFile" "result" "currentSlot"
  ReadINIStr $StartupMode "$ResultFile" "result" "startupMode"
FunctionEnd

Function AppendResultBodyLine
  Exch $0
  ${If} $ResultBodyText == ""
    StrCpy $ResultBodyText "$0"
  ${Else}
    StrCpy $ResultBodyText "$ResultBodyText$\r$\n$0"
  ${EndIf}
  Pop $0
FunctionEnd

Function ResolveStartupModeText
  ${If} $StartupMode == "manual"
    Push "$(STR_STARTUP_MANUAL)"
    Return
  ${EndIf}
  ${If} $StartupMode == "login_autostart"
    Push "$(STR_STARTUP_LOGIN_AUTOSTART)"
    Return
  ${EndIf}
  ${If} $ProbeStartupMode == "manual"
    Push "$(STR_STARTUP_MANUAL)"
    Return
  ${EndIf}
  ${If} $ProbeStartupMode == "login_autostart"
    Push "$(STR_STARTUP_LOGIN_AUTOSTART)"
    Return
  ${EndIf}
  Push ""
FunctionEnd

Function AppendCommonResultDetails
  ${If} $CurrentVersion != ""
    Push "$(STR_DETAIL_VERSION)$CurrentVersion"
    Call AppendResultBodyLine
  ${ElseIf} $ProbeCurrentVersion != ""
    Push "$(STR_DETAIL_VERSION)$ProbeCurrentVersion"
    Call AppendResultBodyLine
  ${EndIf}

  Call ResolveStartupModeText
  Pop $0
  ${If} $0 != ""
    Push "$(STR_DETAIL_STARTUP_MODE)$0"
    Call AppendResultBodyLine
  ${EndIf}
FunctionEnd

Function DeriveResultState
  StrCpy $PrimaryActionKind "finish"
  StrCpy $PrimaryActionTarget ""
  StrCpy $PrimaryButtonText "$(STR_BUTTON_FINISH)"
  StrCpy $ShowAdminLink "false"
  StrCpy $ShowLogsLink "false"

  ${If} $LogPath != ""
    StrCpy $ShowLogsLink "true"
  ${EndIf}
  ${If} $ProbeMode == ""
    StrCpy $ProbeMode $ResultMode
  ${EndIf}

  ${If} $ResultFailure == "true"
    StrCpy $ResultTitleText "$(STR_FINISH_TITLE_FAILURE)"
    ${If} $ErrorText == ""
      StrCpy $ResultBodyText "$(STR_FINISH_SUMMARY_FAILURE_DEFAULT)"
    ${Else}
      StrCpy $ResultBodyText $ErrorText
    ${EndIf}
    Push "$(STR_FINISH_SUMMARY_FAILURE_FOOTER)"
    Call AppendResultBodyLine
    Return
  ${EndIf}

  ${If} $ProbeMode == "first_install"
    ${If} $SetupRequired == "true"
    ${AndIf} $SetupURL != ""
      StrCpy $PrimaryActionKind "continue_websetup"
      StrCpy $PrimaryActionTarget $SetupURL
      StrCpy $PrimaryButtonText "$(STR_BUTTON_CONTINUE_WEBSETUP)"
      StrCpy $ResultTitleText "$(STR_FINISH_TITLE_INSTALL_SETUP)"
      StrCpy $ResultBodyText "$(STR_FINISH_SUMMARY_INSTALL_SETUP)"
      Call AppendCommonResultDetails
      Return
    ${EndIf}

    StrCpy $ResultTitleText "$(STR_FINISH_TITLE_INSTALL_COMPLETE)"
    StrCpy $ResultBodyText "$(STR_FINISH_SUMMARY_INSTALL_COMPLETE)"
    ${If} $AdminURL != ""
      StrCpy $ShowAdminLink "true"
    ${EndIf}
    Call AppendCommonResultDetails
    Return
  ${EndIf}

  ${If} $ProbeSameVersion == "true"
    StrCpy $ResultTitleText "$(STR_FINISH_TITLE_REINSTALL_COMPLETE)"
    StrCpy $ResultBodyText "$(STR_FINISH_SUMMARY_REINSTALL_COMPLETE)"
  ${ElseIf} $CurrentTrack != ""
    StrCpy $ResultTitleText "$(STR_FINISH_TITLE_UPGRADE_COMPLETE)"
    StrCpy $ResultBodyText "$(STR_FINISH_SUMMARY_UPGRADE_COMPLETE)"
  ${Else}
    StrCpy $ResultTitleText "$(STR_FINISH_TITLE_REPAIR_COMPLETE)"
    StrCpy $ResultBodyText "$(STR_FINISH_SUMMARY_REPAIR_COMPLETE)"
  ${EndIf}

  ${If} $AdminURL != ""
    StrCpy $ShowAdminLink "true"
  ${EndIf}
  Call AppendCommonResultDetails
FunctionEnd

Function FinishPageShow
  SendMessage $mui.Button.Next ${WM_SETTEXT} 0 "STR:$PrimaryButtonText"
  SendMessage $mui.FinishPage.Title ${WM_SETTEXT} 0 "STR:$ResultTitleText"
  SendMessage $mui.FinishPage.Text ${WM_SETTEXT} 0 "STR:$ResultBodyText"

  ${If} $ShowAdminLink == "true"
    ${If} $ShowLogsLink == "true"
      ${NSD_CreateLink} 120u 160u 195u 10u "$(STR_LINK_OPEN_ADMIN)"
    ${Else}
      ${NSD_CreateLink} 120u 167u 195u 10u "$(STR_LINK_OPEN_ADMIN)"
    ${EndIf}
    Pop $FinishAdminLink
    SetCtlColors $FinishAdminLink "000080" "${MUI_BGCOLOR}"
    ${NSD_OnClick} $FinishAdminLink FinishPageOpenAdmin
  ${EndIf}

  ${If} $ShowLogsLink == "true"
    ${If} $ShowAdminLink == "true"
      ${NSD_CreateLink} 120u 174u 195u 10u "$(STR_LINK_OPEN_LOGS)"
    ${Else}
      ${NSD_CreateLink} 120u 167u 195u 10u "$(STR_LINK_OPEN_LOGS)"
    ${EndIf}
    Pop $FinishLogsLink
    SetCtlColors $FinishLogsLink "000080" "${MUI_BGCOLOR}"
    ${NSD_OnClick} $FinishLogsLink FinishPageOpenLogs
  ${EndIf}
FunctionEnd

Function FinishPageLeave
  Call PerformPrimaryAction
  ${If} $ResultFailure == "true"
    SetErrorLevel 1
  ${EndIf}
FunctionEnd

Function PerformPrimaryAction
  ${If} $PrimaryActionKind != "continue_websetup"
    Return
  ${EndIf}
  ${If} $PrimaryActionTarget == ""
    Return
  ${EndIf}
  ${If} $TestCaptureFile != ""
    Push "$PrimaryActionTarget"
    Call AppendCaptureOpenedTarget
    Return
  ${EndIf}
  ExecShell "open" "$PrimaryActionTarget"
FunctionEnd

Function FinishPageOpenAdmin
  ${If} $AdminURL != ""
    ExecShell "open" "$AdminURL"
  ${EndIf}
FunctionEnd

Function FinishPageOpenLogs
  ${If} $LogPath != ""
    ExecShell "open" "$LogPath"
  ${EndIf}
FunctionEnd

Function AppendCaptureOpenedTarget
  Exch $0
  ${If} $TestCaptureFile != ""
    FileOpen $1 "$TestCaptureFile" a
    FileWriteUTF16LE $1 "openedTarget=$0$\r$\n"
    FileClose $1
  ${EndIf}
  Pop $0
FunctionEnd

Function WriteTestCapture
  ${If} $TestCaptureFile == ""
    Return
  ${EndIf}

  Delete "$TestCaptureFile"
  FileOpen $0 "$TestCaptureFile" w
  FileWriteUTF16LE /BOM $0 "probeMode=$ProbeMode$\r$\n"
  FileWriteUTF16LE $0 "probeSameVersion=$ProbeSameVersion$\r$\n"
  FileWriteUTF16LE $0 "resultFailure=$ResultFailure$\r$\n"
  FileWriteUTF16LE $0 "primaryActionKind=$PrimaryActionKind$\r$\n"
  FileWriteUTF16LE $0 "primaryActionTarget=$PrimaryActionTarget$\r$\n"
  FileWriteUTF16LE $0 "primaryButtonText=$PrimaryButtonText$\r$\n"
  FileWriteUTF16LE $0 "titleText=$ResultTitleText$\r$\n"
  FileWriteUTF16LE $0 "bodyText=$ResultBodyText$\r$\n"
  FileWriteUTF16LE $0 "showAdminLink=$ShowAdminLink$\r$\n"
  FileWriteUTF16LE $0 "showLogsLink=$ShowLogsLink$\r$\n"
  FileWriteUTF16LE $0 "adminLinkText=$(STR_LINK_OPEN_ADMIN)$\r$\n"
  FileWriteUTF16LE $0 "logsLinkText=$(STR_LINK_OPEN_LOGS)$\r$\n"
  FileWriteUTF16LE $0 "adminURL=$AdminURL$\r$\n"
  FileWriteUTF16LE $0 "setupURL=$SetupURL$\r$\n"
  FileWriteUTF16LE $0 "logPath=$LogPath$\r$\n"
  FileClose $0
FunctionEnd
