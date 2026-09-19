Unicode true

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
!ifndef STAGEDIR
  !error "STAGEDIR is required"
!endif
!ifndef OUTFILE
  !define OUTFILE "..\dist\MCP-DevDesk-Setup-x64.exe"
!endif

Name "MCP DevDesk"
OutFile "${OUTFILE}"
InstallDir "$PROGRAMFILES64\MCP DevDesk"
InstallDirRegKey HKLM "Software\MCP DevDesk" "InstallDir"
RequestExecutionLevel admin
SetCompressor /SOLID lzma
VIProductVersion "${VERSION}.0"
VIAddVersionKey /LANG=1033 "ProductName" "MCP DevDesk"
VIAddVersionKey /LANG=1033 "FileDescription" "MCP DevDesk Setup"
VIAddVersionKey /LANG=1033 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=1033 "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=1033 "CompanyName" "MCP DevDesk"

Page directory
Page instfiles
UninstPage uninstConfirm
UninstPage instfiles

Section "MCP DevDesk" SEC_MAIN
  SetShellVarContext all
  SetOutPath "$INSTDIR"
  ; cloudflared has its own updater and version lifecycle. Preserve an
  ; existing runtime during MCP DevDesk upgrades so setup cannot downgrade it.
  File /r /x "cloudflared.exe" "${STAGEDIR}\*.*"
  IfFileExists "$INSTDIR\cloudflared.exe" cloudflared_done
  File /oname=cloudflared.exe "${STAGEDIR}\cloudflared.exe"
cloudflared_done:
  WriteRegStr HKLM "Software\MCP DevDesk" "InstallDir" "$INSTDIR"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\MCP DevDesk" "DisplayName" "MCP DevDesk"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\MCP DevDesk" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\MCP DevDesk" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\MCP DevDesk" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateDirectory "$SMPROGRAMS\MCP DevDesk"
  CreateShortcut "$SMPROGRAMS\MCP DevDesk\MCP DevDesk.lnk" "$INSTDIR\MCP-DevDesk.exe"
  CreateShortcut "$SMPROGRAMS\MCP DevDesk\Uninstall MCP DevDesk.lnk" "$INSTDIR\Uninstall.exe"
  CreateShortcut "$DESKTOP\MCP DevDesk.lnk" "$INSTDIR\MCP-DevDesk.exe"
SectionEnd

Section "Uninstall"
  SetShellVarContext all
  Delete "$DESKTOP\MCP DevDesk.lnk"
  Delete "$SMPROGRAMS\MCP DevDesk\MCP DevDesk.lnk"
  Delete "$SMPROGRAMS\MCP DevDesk\Uninstall MCP DevDesk.lnk"
  RMDir "$SMPROGRAMS\MCP DevDesk"
  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\MCP DevDesk"
  DeleteRegKey HKLM "Software\MCP DevDesk"
  RMDir /r "$INSTDIR"
SectionEnd
