; Patchbay NSIS Installer Script
; Generates a lightweight, professional 64-bit Windows setup executable

!define PRODUCT_NAME "Patchbay"
!ifndef PRODUCT_VERSION
  !define PRODUCT_VERSION "1.0.0"
!endif
!define PRODUCT_WEB_SITE "https://github.com/fadlee/patchbay"
!define PRODUCT_EXE "patchbay.exe"
!define PRODUCT_UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${PRODUCT_NAME}"
!define PRODUCT_UNINST_ROOT_KEY "HKLM"
!define SERVICE_NAME "PatchbayPortForwarder"

; Modern UI 2
!include "MUI2.nsh"

; Compression
SetCompressor /SOLID lzma

; General Settings
Name "${PRODUCT_NAME} ${PRODUCT_VERSION}"
OutFile "dist\patchbay-setup-amd64.exe"
InstallDir "$PROGRAMFILES64\patchbay"
InstallDirRegKey HKLM "Software\Patchbay" "InstallDir"
RequestExecutionLevel admin

; UI Configuration & Icons
!define MUI_ICON "assets\icon.ico"
!define MUI_UNICON "assets\icon.ico"
!define MUI_ABORTWARNING

; Installer Pages
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXE}"
!define MUI_FINISHPAGE_RUN_TEXT "Launch ${PRODUCT_NAME} now"
!insertmacro MUI_PAGE_FINISH

; Uninstaller Pages
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

; Language
!insertmacro MUI_LANGUAGE "English"

Section "MainSection" SEC01
    SetOutPath "$INSTDIR"
    SetOverwrite on

    ; Stop existing service/process if running during upgrade
    DetailPrint "Stopping existing service if running..."
    nsExec::Exec 'sc.exe stop "${SERVICE_NAME}"'
    Sleep 1000

    ; Copy binary
    File "/oname=${PRODUCT_EXE}" "dist\patchbay-windows-amd64.exe"
    File "assets\icon.ico"

    ; Write installation path to registry
    WriteRegStr HKLM "Software\Patchbay" "InstallDir" "$INSTDIR"

    ; Create Start Menu shortcuts
    CreateDirectory "$SMPROGRAMS\Patchbay"
    CreateShortcut "$SMPROGRAMS\Patchbay\${PRODUCT_NAME}.lnk" "$INSTDIR\${PRODUCT_EXE}" "" "$INSTDIR\icon.ico" 0
    CreateShortcut "$SMPROGRAMS\Patchbay\Uninstall ${PRODUCT_NAME}.lnk" "$INSTDIR\uninstall.exe" "" "$INSTDIR\uninstall.exe" 0

    ; Desktop shortcut
    CreateShortcut "$DESKTOP\${PRODUCT_NAME}.lnk" "$INSTDIR\${PRODUCT_EXE}" "" "$INSTDIR\icon.ico" 0

    ; Create Uninstaller
    WriteUninstaller "$INSTDIR\uninstall.exe"

    ; Register in Add/Remove Programs
    WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "DisplayName" "${PRODUCT_NAME}"
    WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
    WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "DisplayIcon" '"$INSTDIR\icon.ico"'
    WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "DisplayVersion" "${PRODUCT_VERSION}"
    WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "URLInfoAbout" "${PRODUCT_WEB_SITE}"
    WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "Publisher" "${PRODUCT_PUBLISHER}"
    WriteRegDWORD ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "NoModify" 1
    WriteRegDWORD ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "NoRepair" 1
SectionEnd

Section "Uninstall"
    ; Stop and delete service if installed
    DetailPrint "Stopping and removing Windows Service..."
    nsExec::Exec 'sc.exe stop "${SERVICE_NAME}"'
    Sleep 1500
    nsExec::Exec 'sc.exe delete "${SERVICE_NAME}"'
    Sleep 500

    ; Kill tray process if running
    nsExec::Exec 'taskkill.exe /F /IM "${PRODUCT_EXE}"'

    ; Remove files and shortcuts
    Delete "$DESKTOP\${PRODUCT_NAME}.lnk"
    Delete "$SMPROGRAMS\Patchbay\${PRODUCT_NAME}.lnk"
    Delete "$SMPROGRAMS\Patchbay\Uninstall ${PRODUCT_NAME}.lnk"
    RMDir "$SMPROGRAMS\Patchbay"

    Delete "$INSTDIR\${PRODUCT_EXE}"
    Delete "$INSTDIR\icon.ico"
    Delete "$INSTDIR\uninstall.exe"
    RMDir "$INSTDIR"

    ; Clean Registry entries
    DeleteRegValue HKLM "Software\Microsoft\Windows\CurrentVersion\Run" "PatchbayTray"
    DeleteRegKey ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}"
    DeleteRegKey HKLM "Software\Patchbay"
SectionEnd
