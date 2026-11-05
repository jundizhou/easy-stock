param([Parameter(Mandatory = $true)][string]$InstallDirectory)

$ErrorActionPreference = 'Stop'

try {
    if ($InstallDirectory -notmatch '^(?:[A-Za-z]:[\\/]|\\\\[^\\]+\\[^\\]+(?:\\|$))') {
        throw 'The installation path is not absolute.'
    }
    $directory = [IO.Path]::GetFullPath($InstallDirectory).TrimEnd([char[]]'\/')
    if ($directory -eq [IO.Path]::GetPathRoot($directory).TrimEnd([char[]]'\/')) {
        throw 'The installation path is a volume root.'
    }
    if (-not (Test-Path -LiteralPath $directory -PathType Container)) { exit 0 }
    if (-not (Test-Path -LiteralPath (Join-Path $directory 'Uninstall easy-stock.exe') -PathType Leaf)) { exit 0 }

    $source = Join-Path $directory 'resources\resources\hermes-runtime\source'
    $website = Join-Path $source 'website'
    # These are bundled documentation directories. Refuse junctions in the
    # path so an unusual installation cannot redirect cleanup elsewhere.
    $part = $directory
    foreach ($component in @('resources', 'resources', 'hermes-runtime', 'source', 'website')) {
        if ((Get-Item -LiteralPath $part).Attributes -band [IO.FileAttributes]::ReparsePoint) {
            throw ('A symbolic link or junction was found at ' + $part)
        }
        $part = Join-Path $part $component
        if (-not (Test-Path -LiteralPath $part -PathType Container)) { exit 0 }
    }
    if ((Get-Item -LiteralPath $website).Attributes -band [IO.FileAttributes]::ReparsePoint) {
        throw ('A symbolic link or junction was found at ' + $website)
    }

    Add-Type -TypeDefinition @'
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;

public static class EasyStockLongPathCleanup {
    private const uint Directory = 0x10;
    private const uint ReparsePoint = 0x400;
    private const uint ReadOnly = 0x1;
    private const int NotFound = 2;
    private const int PathNotFound = 3;
    private static readonly IntPtr InvalidHandle = new IntPtr(-1);

    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    private struct FindData {
        public uint attributes;
        public uint createdLow, createdHigh, accessedLow, accessedHigh, writtenLow, writtenHigh;
        public uint sizeHigh, sizeLow, reserved0, reserved1;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 260)] public string name;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 14)] public string shortName;
    }

    [DllImport("kernel32.dll", EntryPoint = "GetFileAttributesW", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern uint GetAttributes(string path);
    [DllImport("kernel32.dll", EntryPoint = "SetFileAttributesW", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern bool SetAttributes(string path, uint attributes);
    [DllImport("kernel32.dll", EntryPoint = "FindFirstFileW", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern IntPtr FindFirst(string pattern, out FindData data);
    [DllImport("kernel32.dll", EntryPoint = "FindNextFileW", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern bool FindNext(IntPtr handle, out FindData data);
    [DllImport("kernel32.dll", EntryPoint = "FindClose", SetLastError = true)]
    private static extern bool FindClose(IntPtr handle);
    [DllImport("kernel32.dll", EntryPoint = "DeleteFileW", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern bool DeleteFile(string path);
    [DllImport("kernel32.dll", EntryPoint = "RemoveDirectoryW", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern bool RemoveDirectory(string path);

    public static void RemoveTree(string path) {
        // Win32's extended prefix bypasses MAX_PATH without changing machine policy.
        string extended = path.StartsWith(@"\\")
            ? @"\\?\UNC\" + path.Substring(2)
            : @"\\?\" + path;
        uint attributes = GetAttributes(extended);
        if (attributes == uint.MaxValue) {
            int error = Marshal.GetLastWin32Error();
            if (error == NotFound || error == PathNotFound) return;
            throw Failure("inspect", path, error);
        }
        if ((attributes & ReparsePoint) != 0) throw new InvalidOperationException("Refusing a junction: " + path);
        DeleteTree(extended, attributes);
    }

    private static void DeleteTree(string path, uint attributes) {
        if ((attributes & Directory) != 0) {
            FindData entry;
            IntPtr handle = FindFirst(path + @"\*", out entry);
            if (handle == InvalidHandle) {
                int error = Marshal.GetLastWin32Error();
                if (error != NotFound) throw Failure("list", path, error);
            } else {
                try {
                    do {
                        if (entry.name == "." || entry.name == "..") continue;
                        string child = path + @"\" + entry.name;
                        if ((entry.attributes & ReparsePoint) != 0) {
                            throw new InvalidOperationException("Refusing a junction: " + child);
                        }
                        DeleteTree(child, entry.attributes);
                    } while (FindNext(handle, out entry));
                    int error = Marshal.GetLastWin32Error();
                    if (error != 18) throw Failure("list", path, error); // ERROR_NO_MORE_FILES
                } finally {
                    FindClose(handle);
                }
            }
            if (!RemoveDirectory(path)) throw Failure("remove directory", path, Marshal.GetLastWin32Error());
        } else {
            if ((attributes & ReadOnly) != 0 && !SetAttributes(path, attributes & ~ReadOnly)) {
                throw Failure("clear read-only flag", path, Marshal.GetLastWin32Error());
            }
            if (!DeleteFile(path)) throw Failure("delete file", path, Marshal.GetLastWin32Error());
        }
    }

    private static Exception Failure(string operation, string path, int error) {
        return new Win32Exception(error, operation + " failed for " + path + " (Win32 error " + error + ")");
    }
}
'@

    foreach ($name in @('docs', 'i18n')) {
        [EasyStockLongPathCleanup]::RemoveTree((Join-Path $website $name))
    }
    Write-Output 'Legacy Hermes documentation paths removed before upgrade.'
    exit 0
} catch {
    Write-Output ('Unable to prepare old installation files for update: ' + $_.Exception.Message)
    exit 1
}
