param(
    [string]$Source
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)

if ([string]::IsNullOrWhiteSpace($Source)) {
    $preferredSource = Join-Path $Root "new-logo.png"
    if (Test-Path -LiteralPath $preferredSource) {
        $Source = $preferredSource
    } else {
        $Source = Get-ChildItem -LiteralPath (Join-Path $Root "logo") -File -Filter "*.png" |
            Sort-Object Name |
            Select-Object -First 1 -ExpandProperty FullName
    }
}

if (-not $Source -or -not (Test-Path -LiteralPath $Source)) {
    throw "No PNG logo was found in the logo directory."
}

Add-Type -AssemblyName System.Drawing

$PublicDir = Join-Path $Root "frontend\public"
$DesktopAssetDir = Join-Path $Root "app\internal\desktop\assets"
New-Item -ItemType Directory -Force -Path $PublicDir, $DesktopAssetDir | Out-Null

$sourceImage = [System.Drawing.Image]::FromFile((Resolve-Path -LiteralPath $Source).Path)

function New-ResizedPngBytes {
    param([int]$Size)

    $bitmap = New-Object System.Drawing.Bitmap($Size, $Size, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
    try {
        $graphics.Clear([System.Drawing.Color]::Transparent)
        $graphics.CompositingMode = [System.Drawing.Drawing2D.CompositingMode]::SourceCopy
        $graphics.CompositingQuality = [System.Drawing.Drawing2D.CompositingQuality]::HighQuality
        $graphics.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
        $graphics.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
        $graphics.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
        $graphics.DrawImage($sourceImage, 0, 0, $Size, $Size)

        $stream = New-Object System.IO.MemoryStream
        try {
            $bitmap.Save($stream, [System.Drawing.Imaging.ImageFormat]::Png)
            return $stream.ToArray()
        } finally {
            $stream.Dispose()
        }
    } finally {
        $graphics.Dispose()
        $bitmap.Dispose()
    }
}

try {
    [System.IO.File]::WriteAllBytes((Join-Path $PublicDir "brand-logo.png"), (New-ResizedPngBytes -Size 512))

    $sizes = @(16, 20, 24, 32, 40, 48, 64, 128, 256)
    $images = @()
    foreach ($size in $sizes) {
        [byte[]]$pngBytes = New-ResizedPngBytes -Size $size
        $images += [PSCustomObject]@{ Size = $size; Bytes = $pngBytes }
    }

    $iconStream = New-Object System.IO.MemoryStream
    $writer = New-Object System.IO.BinaryWriter($iconStream)
    try {
        $writer.Write([UInt16]0)
        $writer.Write([UInt16]1)
        $writer.Write([UInt16]$images.Count)

        $offset = 6 + (16 * $images.Count)
        foreach ($image in $images) {
            $dimension = if ($image.Size -ge 256) { [byte]0 } else { [byte]$image.Size }
            $writer.Write($dimension)
            $writer.Write($dimension)
            $writer.Write([byte]0)
            $writer.Write([byte]0)
            $writer.Write([UInt16]1)
            $writer.Write([UInt16]32)
            $writer.Write([UInt32]$image.Bytes.Length)
            $writer.Write([UInt32]$offset)
            $offset += $image.Bytes.Length
        }

        foreach ($image in $images) {
            $writer.Write($image.Bytes)
        }
        $writer.Flush()

        $iconBytes = $iconStream.ToArray()
        [System.IO.File]::WriteAllBytes((Join-Path $PublicDir "favicon.ico"), $iconBytes)
        [System.IO.File]::WriteAllBytes((Join-Path $DesktopAssetDir "mcp-devdesk.ico"), $iconBytes)
    } finally {
        $writer.Dispose()
        $iconStream.Dispose()
    }
} finally {
    $sourceImage.Dispose()
}

Write-Host "Brand assets generated from: $Source" -ForegroundColor Green
