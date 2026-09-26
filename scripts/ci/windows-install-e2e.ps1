# Installs the Windows installer on this machine as a user would, then checks
# the whole life cycle: install, the tray app starting the server and
# TorrServer (with its own GStreamer), first-run setup, upgrade in place,
# quitting, uninstall (keeping settings), reinstall, and uninstall /PURGE.
#
#   pwsh scripts/ci/windows-install-e2e.ps1 dist\Moviestracker-Setup-<version>-x64.exe
#
# It installs for the current user and leaves nothing behind when it passes.
# Run it on a throwaway machine (CI, a VM), not where you use Moviestracker.
param([Parameter(Mandatory)] [string] $Setup)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Setup = (Resolve-Path $Setup).Path
$base = 'http://127.0.0.1:8095'
$app = Join-Path $env:LOCALAPPDATA 'Programs\Moviestracker'
$data = Join-Path $env:LOCALAPPDATA 'Moviestracker'
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$programs = 'Moviestracker', 'moviestracker-server', 'torrserver'

function Step($text) { Write-Host "`n==> $text" }
function Fail($text) {
  Write-Host "FAIL: $text"
  $log = Join-Path $data 'moviestracker.log'
  if (Test-Path $log) { Get-Content $log -Tail 80 | Write-Host }
  exit 1
}

# Location is where a GET of $path redirects ('' when it does not).
function Location($path) { & curl.exe -s -o NUL -w '%{redirect_url}' "$base$path" }

function Wait-Until($what, [scriptblock] $ok, $seconds = 90) {
  $deadline = (Get-Date).AddSeconds($seconds)
  while (-not (& $ok)) {
    if ((Get-Date) -gt $deadline) { Fail "timed out waiting until $what" }
    Start-Sleep -Milliseconds 500
  }
}

function Healthy {
  $body = & curl.exe -fs "$base/healthz" 2>$null
  $LASTEXITCODE -eq 0 -and "$body".StartsWith('ok')
}
function Running($name) { [bool](Get-Process -Name $name -ErrorAction SilentlyContinue) }

function Install {
  $p = Start-Process -FilePath $Setup -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/TASKS=startup' -Wait -PassThru
  if ($p.ExitCode -ne 0) { Fail "the installer exited with $($p.ExitCode)" }
  # Setup starts the tray app, which starts the server.
  Wait-Until 'Moviestracker answers' { Healthy }
}

# Uninstall runs the uninstaller, which hands over to a copy of itself and
# returns at once: wait until the programs are gone.
function Uninstall([string[]] $extra) {
  $arguments = @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART') + $extra
  Start-Process -FilePath (Join-Path $app 'unins000.exe') -ArgumentList $arguments -Wait
  Wait-Until 'the uninstaller has finished' { -not (Test-Path $app) }
}

Step 'Install'
Install
foreach ($file in 'Moviestracker.exe', 'moviestracker-server.exe', 'torrserver.exe', 'unins000.exe', 'LICENSE.txt', 'licenses\TorrServer-LICENSE.txt') {
  if (-not (Test-Path (Join-Path $app $file))) { Fail "$file is not installed" }
}
if (-not (Test-Path (Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\Moviestracker.lnk'))) { Fail 'no Start menu shortcut' }
$run = (Get-ItemProperty $runKey -Name Moviestracker -ErrorAction SilentlyContinue).Moviestracker
if ($run -ne "`"$app\Moviestracker.exe`"") { Fail "start at sign-in is '$run'" }
foreach ($name in $programs) { if (-not (Running $name)) { Fail "$name is not running" } }
if (-not (Test-Path (Join-Path $data 'moviestracker.log'))) { Fail 'no log in the data folder' }

Step "TorrServer's API stays on this computer"
$cmd = (Get-CimInstance Win32_Process -Filter "Name = 'torrserver.exe'").CommandLine
if ($cmd -notmatch '--port (\d+)') { Fail "cannot find TorrServer's API port in: $cmd" }
$apiPort = [int]$Matches[1]
$listen = @(Get-NetTCPConnection -State Listen -LocalPort $apiPort | Select-Object -ExpandProperty LocalAddress -Unique)
if ($listen.Count -ne 1 -or $listen[0] -ne '127.0.0.1') { Fail "TorrServer's API listens on $($listen -join ', ')" }

Step 'TorrServer plays MKV in the browser with the GStreamer it carries'
$password = (Get-Content (Join-Path $data 'engine\accs.db') -Raw | ConvertFrom-Json).moviestracker
$auth = 'moviestracker:' + $password
$gst = $null
Wait-Until 'GStreamer works' {
  $json = & curl.exe -s -u $auth "http://127.0.0.1:$apiPort/gst/echo" 2>$null
  if ($LASTEXITCODE -ne 0 -or -not $json) { return $false }
  $script:gst = $json | ConvertFrom-Json
  $gst.gstreamer.available -and $gst.gstreamer.works
} 180
Write-Host "GStreamer $($gst.gstreamer.version), HDR to SDR: $($gst.hdr_tone_mapping.works)"

Step 'First-run setup from this computer'
if ((Location '/movies') -ne "$base/setup") { Fail 'a fresh install does not send visitors to setup' }
$secret = -join ((1..32) | ForEach-Object { '{0:x}' -f (Get-Random -Maximum 16) })
$body = @{ accepted = $true; username = 'ci'; password = $secret } | ConvertTo-Json -Compress
$body | & curl.exe -fsS -X POST "$base/api/setup" -H 'Content-Type: application/json' -H 'Datastar-Request: true' --data-binary '@-' | Out-Null
if ((Location '/setup') -ne "$base/login") { Fail 'setup did not create the administrator' }

Step 'Upgrade in place keeps the accounts'
Install
if ((Location '/setup') -ne "$base/login") { Fail 'the upgrade lost the administrator' }

Step 'Quit stops Moviestracker, its server and TorrServer'
$p = Start-Process -FilePath (Join-Path $app 'Moviestracker.exe') -ArgumentList '--quit' -Wait -PassThru
if ($p.ExitCode -ne 0) { Fail "--quit exited with $($p.ExitCode)" }
foreach ($name in $programs) { if (Running $name) { Fail "$name still runs after Quit" } }
if (Healthy) { Fail 'something still answers on 8095' }

Step 'Opening it again starts everything'
Start-Process -FilePath (Join-Path $app 'Moviestracker.exe')
Wait-Until 'Moviestracker answers' { Healthy }
if (-not (Running 'torrserver')) { Fail 'TorrServer did not start again' }

Step 'Uninstall keeps settings, removes the programs, TorrServer and its GStreamer'
Uninstall @()
foreach ($name in $programs) { if (Running $name) { Fail "$name still runs" } }
if (Get-ItemProperty $runKey -Name Moviestracker -ErrorAction SilentlyContinue) { Fail 'start at sign-in is left behind' }
if (Test-Path (Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\Moviestracker.lnk')) { Fail 'the Start menu shortcut is left behind' }
if (Test-Path (Join-Path $data 'engine')) { Fail "TorrServer's data is left behind" }
if (Get-ChildItem (Join-Path $env:LOCALAPPDATA 'TorrServer') -Filter 'gst-lib-*' -ErrorAction SilentlyContinue) { Fail "TorrServer's GStreamer is left behind" }
if (-not (Test-Path (Join-Path $data 'moviestracker.json'))) { Fail 'the settings were not kept' }

Step 'Reinstall picks the settings up again'
Install
if ((Location '/setup') -ne "$base/login") { Fail 'the reinstall lost the administrator' }

Step 'Uninstall /PURGE removes everything'
Uninstall @('/PURGE')
if (Test-Path $data) { Fail "$data is left behind after /PURGE" }
if (Healthy) { Fail 'something still answers on 8095' }

Write-Host "`nThe Windows install, upgrade, quit, uninstall and purge all work."
