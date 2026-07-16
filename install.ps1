# Aida Windows CLI installer.
#
# Usage:
#   Invoke-RestMethod http://<host>/statics-live/aida/install.ps1 | Invoke-Expression
#
# Optional environment variables:
#   AIDA_RELEASE_URL  Static release directory URL, defaults to the packaged value.
#   AIDA_API_URL      Aida API base URL written to %USERPROFILE%\.aida.yaml. Defaults to the packaged value.
#   AIDA_INTERNAL_API_URL  Optional trusted internal API URL preferred when authenticated probing succeeds.
#   AIDA_TOKEN        User JWT written to %USERPROFILE%\.aida.yaml when provided.
#   AIDA_INSTALL_DIR  Install directory, default %LOCALAPPDATA%\Aida\bin.
#   AIDA_FORCE        Set to 1 to skip update prompts.

$ErrorActionPreference = "Stop"

$DefaultReleaseUrl = "http://localhost:5080/statics-live/aida"
$DefaultApiUrl = "http://localhost:8080/api/v1"
$DefaultInternalApiUrl = ""

function Resolve-Value($Value, $Default) {
    if ([string]::IsNullOrWhiteSpace($Value)) {
        return $Default
    }
    return $Value
}

function Add-UserPath($PathToAdd) {
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ([string]::IsNullOrWhiteSpace($userPath)) {
        $items = @()
    } else {
        $items = $userPath -split ';' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    }

    $alreadyAdded = $false
    foreach ($item in $items) {
        if ($item.TrimEnd('\') -ieq $PathToAdd.TrimEnd('\')) {
            $alreadyAdded = $true
            break
        }
    }

    if (-not $alreadyAdded) {
        $newPath = if ([string]::IsNullOrWhiteSpace($userPath)) { $PathToAdd } else { "$userPath;$PathToAdd" }
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
        $env:Path = "$env:Path;$PathToAdd"
        return $true
    }
    return $false
}

function Test-PathListContains($PathList, $PathToFind) {
    if ([string]::IsNullOrWhiteSpace($PathList)) {
        return $false
    }

    $target = $PathToFind.TrimEnd('\')
    foreach ($item in ($PathList -split ';')) {
        if (-not [string]::IsNullOrWhiteSpace($item) -and $item.TrimEnd('\') -ieq $target) {
            return $true
        }
    }
    return $false
}

function Update-CurrentProcessPath($PathToAdd) {
    $machinePath = [Environment]::GetEnvironmentVariable("Path", "Machine")
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $pathParts = @($machinePath, $userPath) | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    $combinedPath = [string]::Join(';', $pathParts)

    if ([string]::IsNullOrWhiteSpace($combinedPath)) {
        $combinedPath = $env:Path
    }

    if (-not (Test-PathListContains $combinedPath $PathToAdd)) {
        $combinedPath = if ([string]::IsNullOrWhiteSpace($combinedPath)) { $PathToAdd } else { "$combinedPath;$PathToAdd" }
    }

    $env:Path = $combinedPath
}

if (-not [Environment]::Is64BitOperatingSystem) {
    throw "Unsupported Windows architecture. Current release only provides windows/amd64."
}

$releaseUrl = (Resolve-Value $env:AIDA_RELEASE_URL $DefaultReleaseUrl).TrimEnd('/')
$apiUrl = (Resolve-Value $env:AIDA_API_URL $DefaultApiUrl).TrimEnd('/')
$internalApiUrl = (Resolve-Value $env:AIDA_INTERNAL_API_URL $DefaultInternalApiUrl).TrimEnd('/')
$token = $env:AIDA_TOKEN
$installDir = Resolve-Value $env:AIDA_INSTALL_DIR (Join-Path $env:LOCALAPPDATA "Aida\bin")
$aidaExe = Join-Path $installDir "aida.exe"

Write-Host "=== Aida Installer ==="
Write-Host "  Release URL: $releaseUrl"
Write-Host "  Install dir: $installDir"
Write-Host "  API URL:     $apiUrl"
if (-not [string]::IsNullOrWhiteSpace($internalApiUrl)) {
    Write-Host "  Internal API:$internalApiUrl"
}
Write-Host ""

$version = (Invoke-RestMethod "$releaseUrl/aida-latest.txt").ToString().Trim()
if ([string]::IsNullOrWhiteSpace($version)) {
    throw "failed to fetch aida-latest.txt"
}

New-Item -ItemType Directory -Force -Path $installDir | Out-Null

$needInstall = $true
if (Test-Path $aidaExe) {
    try {
        $current = (& $aidaExe version 2>$null | Select-Object -First 1).ToString().Split(' ')[1]
    } catch {
        $current = ""
    }

    if ($current -eq $version) {
        Write-Host "aida v$version already installed, skipping binary download"
        $needInstall = $false
    } elseif ($env:AIDA_FORCE -ne "1") {
        Write-Host "aida update available: $current -> $version"
    }
}

if ($needInstall) {
    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("aida-windows-amd64-{0}.exe" -f ([Guid]::NewGuid().ToString("N")))
    Write-Host "downloading aida-windows-amd64.exe ..."
    Invoke-WebRequest -Uri "$releaseUrl/aida-windows-amd64.exe" -OutFile $tmp

    $size = (Get-Item $tmp).Length
    if ($size -lt 1048576) {
        Remove-Item -Force $tmp
        throw "downloaded binary is too small ($size bytes). Check $releaseUrl/aida-windows-amd64.exe"
    }

    Move-Item -Force $tmp $aidaExe
    Write-Host "installed aida v$version -> $aidaExe"
}

$pathChanged = Add-UserPath $installDir
Update-CurrentProcessPath $installDir
if ($pathChanged) {
    Write-Host "added $installDir to the current user's PATH"
}

function Set-AidaConfigValue($Lines, $Key, $Value) {
    $result = New-Object System.Collections.Generic.List[string]
    $replaced = $false
    foreach ($line in $Lines) {
        if ($line -match "^\s*$([regex]::Escape($Key))\s*:") {
            if (-not $replaced) {
                $result.Add("${Key}: $Value")
                $replaced = $true
            }
            continue
        }
        $result.Add($line)
    }
    if (-not $replaced) {
        $result.Add("${Key}: $Value")
    }
    return $result.ToArray()
}

$configFile = Join-Path $env:USERPROFILE ".aida.yaml"
if (Test-Path $configFile) {
    $lines = @(Get-Content -Path $configFile)
    if (-not [string]::IsNullOrWhiteSpace($apiUrl)) {
        $lines = @(Set-AidaConfigValue $lines "api_url" $apiUrl)
    }
    if (-not [string]::IsNullOrWhiteSpace($internalApiUrl)) {
        $lines = @(Set-AidaConfigValue $lines "internal_api_url" $internalApiUrl)
        $lines = @(Set-AidaConfigValue $lines "auto_route" "true")
    }
    $lines = @(Set-AidaConfigValue $lines "release_url" $releaseUrl)
    $lines = @(Set-AidaConfigValue $lines "auto_update" "true")
    if (-not [string]::IsNullOrWhiteSpace($token)) {
        $lines = @(Set-AidaConfigValue $lines "token" $token)
    }
    Set-Content -Path $configFile -Value $lines -Encoding UTF8
    Write-Host "updated config -> $configFile"
} elseif (-not [string]::IsNullOrWhiteSpace($apiUrl) -or -not [string]::IsNullOrWhiteSpace($token)) {
    $lines = @()
    if (-not [string]::IsNullOrWhiteSpace($apiUrl)) {
        $lines += "api_url: $apiUrl"
    }
    if (-not [string]::IsNullOrWhiteSpace($internalApiUrl)) {
        $lines += "internal_api_url: $internalApiUrl"
        $lines += "auto_route: true"
    }
    $lines += "release_url: $releaseUrl"
    $lines += "auto_update: true"
    if (-not [string]::IsNullOrWhiteSpace($token)) {
        $lines += "token: $token"
    }
    Set-Content -Path $configFile -Value $lines -Encoding UTF8
    Write-Host "wrote config -> $configFile"
}

Write-Host ""
Write-Host "=== Installation complete ==="
if (Get-Command aida -ErrorAction SilentlyContinue) {
    Write-Host "aida is available in this PowerShell session."
} else {
    Write-Host "aida.exe is installed, but this parent shell did not receive the PATH update."
    Write-Host "Run in the current PowerShell session:"
    Write-Host '  $env:Path = [Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [Environment]::GetEnvironmentVariable("Path", "User")'
}
$hasConfigToken = $false
if (Test-Path $configFile) {
    $hasConfigToken = Select-String -Path $configFile -Pattern '^\s*token\s*:\s*\S+' -Quiet
}
if (-not $hasConfigToken) {
    Write-Host "Login: aida login --server $apiUrl --token <jwt>"
}
Write-Host "List local sessions: aida sessions"
Write-Host "Upload sessions:     aida upload --all"
