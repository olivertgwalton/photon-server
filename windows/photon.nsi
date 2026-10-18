; photon.nsi makes Photon's installer: makensis /DVERSION=v1.2.3 /DAPP=folder /DOUT=setup.exe.
; It installs for the signed-in user alone, so it never asks for admin, and replaces the program
; and never the data in %LOCALAPPDATA%\Photon. Run silently (/S), as an update runs it, it asks
; nothing. Either way it starts Photon when it is done.
Unicode true
!include MUI2.nsh
!include WinMessages.nsh

Name "Photon"
OutFile "${OUT}"
RequestExecutionLevel user
InstallDir "$LOCALAPPDATA\Programs\Photon"
SetCompressor /SOLID lzma
!define MUI_ICON "${APP}\Photon.ico"
!define MUI_UNICON "${APP}\Photon.ico"
!define UNINSTALL "Software\Microsoft\Windows\CurrentVersion\Uninstall\Photon"

!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

; Quit asks a running Photon to quit, as closing it does, which stops its server and databases,
; and waits for it to be gone, so its files can be replaced.
!macro Quit un
Function ${un}Quit
  FindWindow $0 "Photon"
  StrCmp $0 0 done
  SendMessage $0 ${WM_CLOSE} 0 0 /TIMEOUT=120000
  StrCpy $1 0
  wait:
    FindWindow $0 "Photon"
    StrCmp $0 0 done
    IntOp $1 $1 + 1
    IntCmp $1 240 done
    Sleep 500
    Goto wait
  done:
  ; The process outlives its window by a moment.
  Sleep 1000
FunctionEnd
!macroend
!insertmacro Quit ""
!insertmacro Quit "un."

Section
  Call Quit
  RMDir /r "$INSTDIR\photon-server"
  RMDir /r "$INSTDIR\postgres"
  RMDir /r "$INSTDIR\valkey"
  SetOutPath "$INSTDIR"
  File /r "${APP}\*"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateShortcut "$SMPROGRAMS\Photon.lnk" "$INSTDIR\Photon.exe" "" "$INSTDIR\Photon.ico"
  WriteRegStr HKCU "${UNINSTALL}" "DisplayName" "Photon"
  WriteRegStr HKCU "${UNINSTALL}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINSTALL}" "DisplayIcon" "$INSTDIR\Photon.ico"
  WriteRegStr HKCU "${UNINSTALL}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTALL}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKCU "${UNINSTALL}" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  WriteRegDWORD HKCU "${UNINSTALL}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTALL}" "NoRepair" 1
  Exec '"$INSTDIR\Photon.exe"'
SectionEnd

; Uninstalling takes the program away and leaves the library's database, as dragging Photon.app
; to the Bin does.
Section "Uninstall"
  Call un.Quit
  RMDir /r "$INSTDIR"
  Delete "$SMPROGRAMS\Photon.lnk"
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "Photon"
  DeleteRegKey HKCU "${UNINSTALL}"
SectionEnd
