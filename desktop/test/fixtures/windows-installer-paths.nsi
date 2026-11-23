Unicode true
Name "Installer path test"
OutFile "installer-paths.exe"
RequestExecutionLevel user
!include MUI2.nsh
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

; Supply the variables, messages and hooks normally provided by electron-builder.
Var packageArch
Var PowerShellPath
LangString installing ${LANG_ENGLISH} "Installing"
LangString decompressionFailed ${LANG_ENGLISH} "Extraction failed"
LangString appCannotBeClosed ${LANG_ENGLISH} "Could not close app"
!define APP_EXECUTABLE_FILENAME "easy-stock.exe"
!define isUpdated `0 == 1`
!macro decompress
!macroend
!include "easy-stock-installer.nsh"

Section
  StrCpy $packageArch "64"
  StrCpy $PowerShellPath "powershell.exe"
  !insertmacro customCheckAppRunning
  !insertmacro decompress
  !insertmacro customInstall
SectionEnd
