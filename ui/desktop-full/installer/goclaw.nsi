; GoClaw for Windows installer (per-machine, unsigned).
;
; Built by scripts/build-winapp.ps1:
;   makensis /DVERSION=<semver> /DVIVERSION=<a.b.c.d> /DSRCDIR=<dir with the exes>
;            /DOUTFILE=<path> goclaw.nsi
;
; Program files go to %ProgramFiles%\GoClaw and are read-only at runtime. The
; app keeps its settings and logs per user, so uninstalling keeps them unless
; the user asks to delete them.

Unicode true
SetCompressor /SOLID lzma
RequestExecutionLevel admin
ManifestDPIAware true

!ifndef VERSION
  !define VERSION "0.0.0-dev"
!endif
!ifndef VIVERSION
  !define VIVERSION "0.0.0.0"
!endif
!ifndef SRCDIR
  !error "pass /DSRCDIR=<folder with GoClawDesktop.exe, goclaw-server.exe, pg_dump.exe>"
!endif
!ifndef OUTFILE
  !define OUTFILE "GoClaw-Setup.exe"
!endif

!define APPNAME "GoClaw"
!define COMPANY "GoClaw"
!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\GoClaw"
!define WEBVIEW2KEY "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"

Name "${APPNAME}"
OutFile "${OUTFILE}"
InstallDir "$PROGRAMFILES64\GoClaw"

VIProductVersion "${VIVERSION}"
VIAddVersionKey "ProductName" "${APPNAME}"
VIAddVersionKey "CompanyName" "${COMPANY}"
VIAddVersionKey "LegalCopyright" "GoClaw"
VIAddVersionKey "FileDescription" "${APPNAME} installer"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "ProductVersion" "${VERSION}"

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"

!define MUI_ICON "goclaw.ico"
!define MUI_UNICON "goclaw.ico"
!define MUI_ABORTWARNING

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
; Launch through explorer so the app starts as the normal user, not elevated.
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "Start GoClaw"
!define MUI_FINISHPAGE_RUN_FUNCTION LaunchApp
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

Function LaunchApp
  Exec '"$WINDIR\explorer.exe" "$INSTDIR\GoClawDesktop.exe"'
FunctionEnd

; Stops the server and refuses to continue while the window is still open.
!macro StopGoClaw
  IfFileExists "$INSTDIR\GoClawDesktop.exe" 0 +2
    ExecWait '"$INSTDIR\GoClawDesktop.exe" --quit'
  ; Fallback for a pid file of another account: stop servers started from this folder.
  nsExec::Exec 'powershell -NoProfile -NonInteractive -Command "Get-Process goclaw-server -ErrorAction SilentlyContinue | Where-Object { $$_.Path -like \"$INSTDIR\*\" } | Stop-Process -Force"'
  Pop $0
!macroend

; Waits until the GoClaw window is closed (find returns 0 while it is listed).
Function CloseWindowCheck
  ${Do}
    nsExec::Exec 'cmd /c tasklist /FI "IMAGENAME eq GoClawDesktop.exe" /NH | find /I "GoClawDesktop.exe"'
    Pop $0
    ${If} $0 != "0"
      ${ExitDo}
    ${EndIf}
    IfSilent 0 +2
      Abort "GoClaw is running; close it and run the installer again."
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "GoClaw is still open. Close its window, then choose Retry." IDRETRY +2
      Abort "Installation cancelled."
  ${Loop}
FunctionEnd

Function .onInit
  SetRegView 64
  ; An upgrade keeps the folder chosen earlier.
  ReadRegStr $0 HKLM "${UNINSTKEY}" "InstallLocation"
  ${If} $0 != ""
    StrCpy $INSTDIR $0
  ${EndIf}
  ; The window needs the WebView2 runtime (part of Windows 11 and current Windows 10).
  ReadRegStr $0 HKLM "${WEBVIEW2KEY}" "pv"
  ${If} $0 == ""
    ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  ${EndIf}
  ${If} $0 == ""
  ${OrIf} $0 == "0.0.0.0"
    MessageBox MB_OK|MB_ICONSTOP "The Microsoft Edge WebView2 runtime is missing. Install it from https://developer.microsoft.com/microsoft-edge/webview2/ and run this installer again."
    Abort
  ${EndIf}
FunctionEnd

Section "GoClaw (required)" SecMain
  SectionIn RO
  SetRegView 64
  SetShellVarContext all
  Call CloseWindowCheck
  !insertmacro StopGoClaw

  SetOutPath "$INSTDIR"
  File "${SRCDIR}\GoClawDesktop.exe"
  File "${SRCDIR}\goclaw-server.exe"
  File "${SRCDIR}\pg_dump.exe"
  File "goclaw.ico"

  CreateDirectory "$SMPROGRAMS\GoClaw"
  CreateShortcut "$SMPROGRAMS\GoClaw\GoClaw.lnk" "$INSTDIR\GoClawDesktop.exe" "" "$INSTDIR\goclaw.ico"
  CreateShortcut "$SMPROGRAMS\GoClaw\Quit GoClaw.lnk" "$INSTDIR\GoClawDesktop.exe" "--quit" "$INSTDIR\goclaw.ico"
  CreateShortcut "$SMPROGRAMS\GoClaw\Uninstall GoClaw.lnk" "$INSTDIR\Uninstall.exe"

  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayName" "${APPNAME}"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINSTKEY}" "Publisher" "${COMPANY}"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\goclaw.ico"
  WriteRegStr HKLM "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "${UNINSTKEY}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKLM "${UNINSTKEY}" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  WriteRegDWORD HKLM "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKLM "${UNINSTKEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKLM "${UNINSTKEY}" "EstimatedSize" "$0"
SectionEnd

Section /o "Desktop shortcut" SecDesktop
  SetShellVarContext all
  CreateShortcut "$DESKTOP\GoClaw.lnk" "$INSTDIR\GoClawDesktop.exe" "" "$INSTDIR\goclaw.ico"
SectionEnd

!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
  !insertmacro MUI_DESCRIPTION_TEXT ${SecMain} "The GoClaw window, the server and the backup helper."
  !insertmacro MUI_DESCRIPTION_TEXT ${SecDesktop} "Add a GoClaw shortcut to the desktop."
!insertmacro MUI_FUNCTION_DESCRIPTION_END

Function un.onInit
  SetRegView 64
FunctionEnd

Section "Uninstall"
  SetRegView 64
  SetShellVarContext all
  !insertmacro StopGoClaw

  Delete "$SMPROGRAMS\GoClaw\GoClaw.lnk"
  Delete "$SMPROGRAMS\GoClaw\Quit GoClaw.lnk"
  Delete "$SMPROGRAMS\GoClaw\Uninstall GoClaw.lnk"
  RMDir "$SMPROGRAMS\GoClaw"
  Delete "$DESKTOP\GoClaw.lnk"

  Delete "$INSTDIR\GoClawDesktop.exe"
  Delete "$INSTDIR\goclaw-server.exe"
  Delete "$INSTDIR\pg_dump.exe"
  Delete "$INSTDIR\goclaw.ico"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"

  DeleteRegKey HKLM "${UNINSTKEY}"
  ; Best effort for the account running the uninstaller; another account's
  ; "start with Windows" entry is left behind and points at a missing file.
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "GoClaw"

  ; Settings and logs belong to the user; keep them unless asked.
  IfSilent done 0
  MessageBox MB_YESNO|MB_ICONQUESTION|MB_DEFBUTTON2 "Also delete your GoClaw settings, logs and backups?$\r$\n$\r$\n(Your database is not touched.)" IDNO done
  SetShellVarContext current
  RMDir /r "$APPDATA\GoClaw"
  RMDir /r "$LOCALAPPDATA\GoClaw"
  done:
SectionEnd
