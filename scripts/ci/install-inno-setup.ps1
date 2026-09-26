# Installs the pinned Inno Setup 7, which builds the Windows installer, and
# puts its compiler (ISCC.exe) on the PATH of the next workflow steps.
$ErrorActionPreference = 'Stop'
$version = '7.1.0'
$sha256 = '0362a383ed217d4c4239b5933866dd96d3eb2102737da92f80f6057a4b40df2f'
$url = "https://github.com/jrsoftware/issrc/releases/download/is-$($version -replace '\.', '_')/innosetup-$version-x64.exe"
$dir = 'C:\InnoSetup7'
$download = Join-Path $env:RUNNER_TEMP 'innosetup.exe'

Invoke-WebRequest -Uri $url -OutFile $download
$got = (Get-FileHash $download -Algorithm SHA256).Hash.ToLowerInvariant()
if ($got -ne $sha256) { throw "Inno Setup $version checksum mismatch: got $got, want $sha256" }
$p = Start-Process -FilePath $download -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', "/DIR=$dir" -Wait -PassThru
if ($p.ExitCode -ne 0) { throw "the Inno Setup installer exited with $($p.ExitCode)" }
if (-not (Test-Path "$dir\ISCC.exe")) { throw "ISCC.exe is not in $dir" }
Add-Content -Path $env:GITHUB_PATH -Value $dir
Write-Host "Inno Setup $version is in $dir"
