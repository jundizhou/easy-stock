!macro customCheckAppRunning
  InitPluginsDir
  Push $0
  Push $1
  Push $2
  Push $3
  File /oname=$PLUGINSDIR\easy-stock-stop-installed-app.ps1 "${BUILD_RESOURCES_DIR}\..\scripts\stop-installed-app.ps1"
  System::Call 'kernel32::GetCurrentProcessId() i.r0'
  StrCpy $1 $0

  easyStockRetryClose:
    nsExec::ExecToStack /TIMEOUT=45000 '"$PowerShellPath" -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\easy-stock-stop-installed-app.ps1" -InstallDirectory "$INSTDIR" -AppExecutable "${APP_EXECUTABLE_FILENAME}" -InstallerProcessId $1'
    Pop $2
    Pop $3
    DetailPrint "$3"
    ${If} $2 != 0
      MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "$(appCannotBeClosed)$\r$\n$\r$\n$3" /SD IDCANCEL IDRETRY easyStockRetryClose
      SetErrorLevel 2
      Quit
    ${EndIf}

  !ifndef BUILD_UNINSTALLER
    ${If} ${isUpdated}
      File /oname=$PLUGINSDIR\easy-stock-repair-legacy-paths.ps1 "${BUILD_RESOURCES_DIR}\..\scripts\repair-legacy-windows-paths.ps1"
      nsExec::ExecToStack /TIMEOUT=120000 '"$PowerShellPath" -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\easy-stock-repair-legacy-paths.ps1" -InstallDirectory "$INSTDIR"'
      Pop $2
      Pop $3
      DetailPrint "$3"
      ${If} $2 != 0
        MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "Unable to prepare old installation files for update.$\r$\n$3" /SD IDCANCEL IDRETRY easyStockRetryClose
        SetErrorLevel 2
        Quit
      ${EndIf}
    ${EndIf}
  !endif

  Pop $3
  Pop $2
  Pop $1
  Pop $0
!macroend
