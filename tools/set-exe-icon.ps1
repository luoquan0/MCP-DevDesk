param(
    [Parameter(Mandatory = $true)]
    [string]$ExePath,

    [Parameter(Mandatory = $true)]
    [string]$IconPath
)

$ErrorActionPreference = "Stop"

$ExePath = (Resolve-Path -LiteralPath $ExePath).Path
$IconPath = (Resolve-Path -LiteralPath $IconPath).Path

Add-Type @"
using System;
using System.Runtime.InteropServices;

public static class NativeResourceUpdater {
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    public static extern IntPtr BeginUpdateResource(string fileName, bool deleteExistingResources);

    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    public static extern bool UpdateResource(
        IntPtr updateHandle,
        IntPtr resourceType,
        IntPtr resourceName,
        ushort language,
        IntPtr data,
        uint dataSize);

    [DllImport("kernel32.dll", SetLastError = true)]
    public static extern bool EndUpdateResource(IntPtr updateHandle, bool discard);
}
"@

function Set-NativeResource {
    param(
        [IntPtr]$Handle,
        [int]$Type,
        [int]$Name,
        [byte[]]$Data
    )

    $memory = [Runtime.InteropServices.Marshal]::AllocHGlobal($Data.Length)
    try {
        [Runtime.InteropServices.Marshal]::Copy($Data, 0, $memory, $Data.Length)
        $ok = [NativeResourceUpdater]::UpdateResource(
            $Handle,
            [IntPtr]$Type,
            [IntPtr]$Name,
            [UInt16]0,
            $memory,
            [UInt32]$Data.Length)
        if (-not $ok) {
            $errorCode = [Runtime.InteropServices.Marshal]::GetLastWin32Error()
            throw "UpdateResource failed for type $Type, name $Name (Win32 error $errorCode)."
        }
    } finally {
        [Runtime.InteropServices.Marshal]::FreeHGlobal($memory)
    }
}

$iconBytes = [System.IO.File]::ReadAllBytes($IconPath)
if ($iconBytes.Length -lt 6 -or
    [BitConverter]::ToUInt16($iconBytes, 0) -ne 0 -or
    [BitConverter]::ToUInt16($iconBytes, 2) -ne 1) {
    throw "The icon file is not a valid ICO container."
}

$count = [BitConverter]::ToUInt16($iconBytes, 4)
if ($count -lt 1) {
    throw "The icon file does not contain any images."
}

$entries = @()
for ($index = 0; $index -lt $count; $index++) {
    $entryOffset = 6 + (16 * $index)
    if ($entryOffset + 16 -gt $iconBytes.Length) {
        throw "The icon directory is truncated."
    }

    $dataLength = [BitConverter]::ToUInt32($iconBytes, $entryOffset + 8)
    $dataOffset = [BitConverter]::ToUInt32($iconBytes, $entryOffset + 12)
    if ($dataOffset + $dataLength -gt $iconBytes.Length) {
        throw "An icon image points outside the ICO container."
    }

    $image = New-Object byte[] $dataLength
    [Array]::Copy($iconBytes, [int]$dataOffset, $image, 0, [int]$dataLength)
    $entries += [PSCustomObject]@{
        Width      = $iconBytes[$entryOffset]
        Height     = $iconBytes[$entryOffset + 1]
        ColorCount = $iconBytes[$entryOffset + 2]
        Reserved   = $iconBytes[$entryOffset + 3]
        Planes     = [BitConverter]::ToUInt16($iconBytes, $entryOffset + 4)
        BitCount   = [BitConverter]::ToUInt16($iconBytes, $entryOffset + 6)
        Data       = [byte[]]$image
        Id         = $index + 1
    }
}

$groupStream = New-Object System.IO.MemoryStream
$groupWriter = New-Object System.IO.BinaryWriter($groupStream)
try {
    $groupWriter.Write([UInt16]0)
    $groupWriter.Write([UInt16]1)
    $groupWriter.Write([UInt16]$entries.Count)
    foreach ($entry in $entries) {
        $groupWriter.Write([byte]$entry.Width)
        $groupWriter.Write([byte]$entry.Height)
        $groupWriter.Write([byte]$entry.ColorCount)
        $groupWriter.Write([byte]$entry.Reserved)
        $groupWriter.Write([UInt16]$entry.Planes)
        $groupWriter.Write([UInt16]$entry.BitCount)
        $groupWriter.Write([UInt32]$entry.Data.Length)
        $groupWriter.Write([UInt16]$entry.Id)
    }
    $groupWriter.Flush()
    [byte[]]$groupData = $groupStream.ToArray()
} finally {
    $groupWriter.Dispose()
    $groupStream.Dispose()
}

$handle = [NativeResourceUpdater]::BeginUpdateResource($ExePath, $false)
if ($handle -eq [IntPtr]::Zero) {
    $errorCode = [Runtime.InteropServices.Marshal]::GetLastWin32Error()
    throw "BeginUpdateResource failed (Win32 error $errorCode)."
}

$committed = $false
try {
    foreach ($entry in $entries) {
        Set-NativeResource -Handle $handle -Type 3 -Name $entry.Id -Data $entry.Data
    }
    Set-NativeResource -Handle $handle -Type 14 -Name 1 -Data $groupData

    if (-not [NativeResourceUpdater]::EndUpdateResource($handle, $false)) {
        $errorCode = [Runtime.InteropServices.Marshal]::GetLastWin32Error()
        throw "EndUpdateResource failed (Win32 error $errorCode)."
    }
    $committed = $true
} finally {
    if (-not $committed) {
        [void][NativeResourceUpdater]::EndUpdateResource($handle, $true)
    }
}

Write-Host "EXE icon updated: $ExePath" -ForegroundColor Green
