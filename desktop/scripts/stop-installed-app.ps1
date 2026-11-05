param(
    [Parameter(Mandatory = $true)][string]$InstallDirectory,
    [ValidatePattern('^[^\\/:]+\.exe$')][string]$AppExecutable = 'easy-stock.exe',
    [int]$InstallerProcessId = 0,
    [ValidateRange(0, 20)][int]$GracePeriodSeconds = 10,
    [ValidateRange(1, 20)][int]$StopTimeoutSeconds = 10
)

$ErrorActionPreference = 'Stop'

try {
    if ($InstallDirectory -notmatch '^(?:[A-Za-z]:[\\/]|\\\\[^\\]+\\[^\\]+(?:\\|$))') {
        throw 'Refusing an installation path that is not absolute.'
    }
    $directory = [IO.Path]::GetFullPath($InstallDirectory).TrimEnd([char[]]'\/')
    $volumeRoot = [IO.Path]::GetPathRoot($directory).TrimEnd([char[]]'\/')
    if ($directory -eq $volumeRoot -or -not [IO.Path]::IsPathRooted($InstallDirectory)) {
        throw 'Refusing an installation path that is a volume root or is not absolute.'
    }
    if (-not (Test-Path -LiteralPath $directory -PathType Container)) {
        Write-Output 'No previous installation at the selected path.'
        exit 0
    }
    if ((Get-Item -LiteralPath $directory).Attributes -band [IO.FileAttributes]::ReparsePoint) {
        throw 'Refusing an installation directory that is a symbolic link or junction.'
    }
    Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
using System.Text;
namespace EasyStock {
    public static class InstallerPaths {
        [DllImport("kernel32.dll", EntryPoint = "GetLongPathNameW", CharSet = CharSet.Unicode, SetLastError = true)]
        public static extern uint GetLongPathName(string path, StringBuilder result, uint capacity);
    }
}
'@
    function Get-LongInstallationPath([string]$Path, [switch]$Required) {
        if (-not $Path) { return '' }
        $buffer = [Text.StringBuilder]::new(32768)
        $length = [EasyStock.InstallerPaths]::GetLongPathName($Path, $buffer, $buffer.Capacity)
        if ($length -gt 0 -and $length -lt $buffer.Capacity) { return $buffer.ToString() }
        if ($Required) { throw 'Unable to normalize the installation directory.' }
        # A process may exit or become inaccessible between snapshots. Keep its
        # original path so an already-long installation path can still match.
        return $Path
    }
    # CIM can report DOS 8.3 names even when the installer used a long path.
    # Expand both sides before comparing, retaining the directory separator
    # boundary so an adjacent installation is never included.
    $directory = (Get-LongInstallationPath $directory -Required).TrimEnd([char[]]'\/')
    $appPath = Join-Path $directory $AppExecutable
    $uninstallerPath = Join-Path $directory ('Uninstall ' + $AppExecutable)
    $backendPath = Join-Path $directory 'resources\resources\backend\easy-stock-backend.exe'
    if (-not (Test-Path -LiteralPath $appPath -PathType Leaf) -and
        -not (Test-Path -LiteralPath $uninstallerPath -PathType Leaf) -and
        -not (Test-Path -LiteralPath $backendPath -PathType Leaf)) {
        Write-Output 'No previous installation files at the selected path.'
        exit 0
    }
    $prefix = $directory + [IO.Path]::DirectorySeparatorChar
    $protected = [Collections.Generic.HashSet[int]]::new()
    $tracked = @{}
    $snapshot = @(Get-CimInstance -ClassName Win32_Process)
    # Never stop this helper, the installer, or an ancestor hosting either one.
    foreach ($initialProcessId in @($PID, $InstallerProcessId)) {
        $currentProcessId = $initialProcessId
        while ($currentProcessId -gt 0 -and $protected.Add($currentProcessId)) {
            $ancestor = $snapshot | Where-Object { $_.ProcessId -eq $currentProcessId } | Select-Object -First 1
            if (-not $ancestor) { break }
            $currentProcessId = [int]$ancestor.ParentProcessId
        }
    }

    function Get-InstallationProcesses {
        $rows = @(Get-CimInstance -ClassName Win32_Process)
        $active = @{}
        foreach ($row in $rows) {
            $rowId = [int]$row.ProcessId
            if ($protected.Contains($rowId) -or -not $row.CreationDate) { continue }
            $identity = $row.CreationDate.ToUniversalTime().Ticks
            $executablePath = Get-LongInstallationPath $row.ExecutablePath
            if (($executablePath -and $executablePath.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) -or
                ($tracked.ContainsKey($rowId) -and $tracked[$rowId] -eq $identity)) {
                $active[$rowId] = $row
                $tracked[$rowId] = $identity
            }
        }
        # Capture descendants before asking their parent to exit. Some runtimes
        # execute outside the installation folder and outlive that parent.
        do {
            $added = $false
            foreach ($row in $rows) {
                $rowId = [int]$row.ProcessId
                if ($protected.Contains($rowId) -or $active.ContainsKey($rowId) -or -not $row.CreationDate) { continue }
                if ($active.ContainsKey([int]$row.ParentProcessId) -and
                    $row.CreationDate -ge $active[[int]$row.ParentProcessId].CreationDate) {
                    $active[$rowId] = $row
                    $tracked[$rowId] = $row.CreationDate.ToUniversalTime().Ticks
                    $added = $true
                }
            }
        } while ($added)
        return @($active.Values)
    }

    $targets = @(Get-InstallationProcesses)
    $requestedClose = $false
    foreach ($target in $targets) {
        $executablePath = Get-LongInstallationPath $target.ExecutablePath
        if (-not [string]::Equals($executablePath, $appPath, [StringComparison]::OrdinalIgnoreCase)) { continue }
        try {
            $appProcess = [Diagnostics.Process]::GetProcessById([int]$target.ProcessId)
            if ($appProcess.CloseMainWindow()) { $requestedClose = $true }
            $appProcess.Dispose()
        } catch [ArgumentException] { }
    }
    if ($requestedClose) {
        $graceDeadline = [DateTime]::UtcNow.AddSeconds($GracePeriodSeconds)
        do {
            $targets = @(Get-InstallationProcesses)
            if ($targets.Count -eq 0) { break }
            Start-Sleep -Milliseconds 200
        } while ([DateTime]::UtcNow -lt $graceDeadline)
    }

    $stopDeadline = [DateTime]::UtcNow.AddSeconds($StopTimeoutSeconds)
    do {
        $targets = @(Get-InstallationProcesses)
        if ($targets.Count -eq 0) {
            Write-Output ('Installation processes stopped; verified {0} process identities.' -f $tracked.Count)
            exit 0
        }
        # Youngest descendants first; their parent remains available for discovery.
        foreach ($target in ($targets | Sort-Object CreationDate -Descending)) {
            $targetProcess = $null
            try {
                $targetProcess = [Diagnostics.Process]::GetProcessById([int]$target.ProcessId)
                if ([Math]::Abs(($targetProcess.StartTime.ToUniversalTime() - $target.CreationDate.ToUniversalTime()).TotalMilliseconds) -gt 10) { continue }
                Stop-Process -InputObject $targetProcess -Force
            } catch [ArgumentException] { }
            catch [InvalidOperationException] { }
            catch {
                Write-Output ('Cannot stop PID {0} ({1}): {2}' -f $target.ProcessId, $target.ExecutablePath, $_.Exception.Message)
            } finally {
                if ($targetProcess) { $targetProcess.Dispose() }
            }
        }
        Start-Sleep -Milliseconds 200
    } while ([DateTime]::UtcNow -lt $stopDeadline)

    $remaining = @(Get-InstallationProcesses)
    if ($remaining.Count -eq 0) { exit 0 }
    $details = ($remaining | ForEach-Object { '{0} (PID {1})' -f $_.ExecutablePath, $_.ProcessId }) -join '; '
    throw ('Installation files may still be in use: ' + $details)
} catch {
    Write-Output ('Unable to prepare this installation directory: ' + $_.Exception.Message)
    exit 1
}
