Unicode true
RequestExecutionLevel user
SetCompressor /SOLID lzma

!define PRODUCT_NAME "MCP DevDesk"
!define PRODUCT_VERSION "0.13.0-beta.1"
!define PRODUCT_PUBLISHER "MCP DevDesk"
!define PRODUCT_EXE "MCP-DevDesk-amd64.exe"
!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\MCP DevDesk"

Name "${PRODUCT_NAME} ${PRODUCT_VERSION}"
OutFile "..\dist\MCP-DevDesk-Setup-x64.exe"
InstallDir "$LOCALAPPDATA\MCP DevDesk"
InstallDirRegKey HKCU "Software\MCP DevDesk" "InstallDir"
ShowInstDetails show
ShowUninstDetails show

Page directory
Page instfiles
UninstPage uninstConfirm
UninstPage instfiles

Section "MCP DevDesk" SEC_MAIN
  SetOutPath "$INSTDIR"
  File /r "..\dist\installer-staging\*.*"

  WriteRegStr HKCU "Software\MCP DevDesk" "InstallDir" "$INSTDIR"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayName" "${PRODUCT_NAME}"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${PRODUCT_VERSION}"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "Publisher" "${PRODUCT_PUBLISHER}"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXE}"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoRepair" 1

  CreateDirectory "$SMPROGRAMS\MCP DevDesk"
  CreateShortcut "$SMPROGRAMS\MCP DevDesk\MCP DevDesk.lnk" "$INSTDIR\${PRODUCT_EXE}"
  CreateShortcut "$SMPROGRAMS\MCP DevDesk\卸载 MCP DevDesk.lnk" "$INSTDIR\Uninstall.exe"
  CreateShortcut "$DESKTOP\MCP DevDesk.lnk" "$INSTDIR\${PRODUCT_EXE}"
SectionEnd

Section "Uninstall"
  Delete "$DESKTOP\MCP DevDesk.lnk"
  Delete "$SMPROGRAMS\MCP DevDesk\MCP DevDesk.lnk"
  Delete "$SMPROGRAMS\MCP DevDesk\卸载 MCP DevDesk.lnk"
  RMDir "$SMPROGRAMS\MCP DevDesk"

  ; Preserve user data if it is stored under the installation directory.
  ; Remove known application binaries/resources while leaving data\devdesk intact.
  Delete "$INSTDIR\MCP-DevDesk-amd64.exe"
  Delete "$INSTDIR\mcp-core-amd64.exe"
  Delete "$INSTDIR\devdeskctl-amd64.exe"
  Delete "$INSTDIR\devdesk-updater-amd64.exe"
  Delete "$INSTDIR\coding-tools-mcp.exe"
  Delete "$INSTDIR\cloudflared.exe"
  Delete "$INSTDIR\Uninstall.exe"

  DeleteRegKey HKCU "${UNINSTALL_KEY}"
  DeleteRegKey HKCU "Software\MCP DevDesk"
SectionEnd
