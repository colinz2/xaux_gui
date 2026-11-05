$ErrorActionPreference = 'Stop'

Push-Location $PSScriptRoot
try {
    if ((go env GOOS) -ne 'windows' -or (go env CGO_ENABLED) -ne '1') {
        throw 'This build requires GOOS=windows and CGO_ENABLED=1 with a working C compiler.'
    }
    $arch = go env GOARCH
    if ($arch -eq 'amd64') { $vadArch = 'x64' }
    elseif ($arch -eq '386') { $vadArch = 'x86' }
    else { throw "TEN VAD does not support Windows architecture $arch." }
    $vadRoot = Join-Path $PSScriptRoot 'pkg\vad\ten-vad'
    $vadDll = Join-Path $vadRoot "lib\Windows\$vadArch\ten_vad.dll"
    if (-not (Test-Path -LiteralPath $vadDll)) {
        throw 'Missing TEN VAD library. Clone TEN-framework/ten-vad into pkg\vad\ten-vad first.'
    }
    go build -tags tenvad -ldflags '-H=windowsgui' -o main.exe .\src
    if ($LASTEXITCODE -ne 0) {
        throw "Windows GUI build failed (exit code $LASTEXITCODE)."
    }
    Copy-Item -LiteralPath $vadDll -Destination (Join-Path $PSScriptRoot 'ten_vad.dll') -Force
    Copy-Item -LiteralPath (Join-Path $vadRoot 'LICENSE') -Destination (Join-Path $PSScriptRoot 'TEN-VAD-LICENSE') -Force
    Copy-Item -LiteralPath (Join-Path $vadRoot 'NOTICES') -Destination (Join-Path $PSScriptRoot 'TEN-VAD-NOTICES') -Force
    Write-Output "Built $PSScriptRoot\main.exe"
}
finally {
    Pop-Location
}
