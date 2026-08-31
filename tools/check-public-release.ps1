param()

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$failed = $false
Push-Location $Root
try {
    $tracked = @(git ls-files)
    if ($LASTEXITCODE -ne 0) {
        throw "git ls-files failed"
    }

    $forbiddenPathPattern = '(?i)(^|/)(\.env($|\.)|secrets\.json$|credentials?\.json$|config\.json$|[^/]+\.(pem|key|pfx|p12)$|id_rsa$|id_ed25519$|\.npmrc$|\.pypirc$)'
    $forbidden = @($tracked | Where-Object { $_ -match $forbiddenPathPattern })
    if ($forbidden.Count -gt 0) {
        throw "Sensitive files are tracked by Git: $($forbidden -join ', ')"
    }

    $secretPatterns = @(
        '-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----',
        'gh[pousr]_[A-Za-z0-9]{30,}',
        'github_pat_[A-Za-z0-9_]{30,}',
        'AKIA[0-9A-Z]{16}'
    )
    foreach ($pattern in $secretPatterns) {
        $matches = @(git grep -l -I -E -- $pattern -- ':!app/vendor/**' ':!app/internal/web/static/**' 2>$null)
        $grepExitCode = $LASTEXITCODE
        if ($grepExitCode -eq 0 -and $matches.Count -gt 0) {
            throw "Potential credential material matched $pattern in: $($matches -join ', ')"
        }
        if ($grepExitCode -ne 0 -and $grepExitCode -ne 1) {
            throw "git grep failed while checking public-release secrets"
        }
    }

    Write-Host "Public release secret guard passed." -ForegroundColor Green
} catch {
    $failed = $true
    throw
} finally {
    Pop-Location
    if (-not $failed) {
        $global:LASTEXITCODE = 0
    }
}
