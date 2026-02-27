param(
    [string]$ProjectDir = "C:\git\coco",
    [string]$UserId = "kermi",
    [string]$Platform = "wecom",
    [string]$Server = "wss://www.greatquant.com/ws",
    [string]$Webhook = "https://www.greatquant.com/webhook",
    [string]$Token = "",
    [int]$RestartDelaySeconds = 5
)

if ([string]::IsNullOrWhiteSpace($Token)) {
    $Token = $env:RELAY_TOKEN
}

if ([string]::IsNullOrWhiteSpace($Token)) {
    throw "relay token is empty. Pass -Token or set RELAY_TOKEN."
}

$exe = Join-Path $ProjectDir "coco.exe"
if (-not (Test-Path $exe)) {
    throw "coco executable not found: $exe"
}

$logPath = Join-Path $ProjectDir "relay-service.log"
Set-Location $ProjectDir

while ($true) {
    $start = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    Add-Content -Path $logPath -Value "$start [relay-service] starting relay"

    & $exe relay --platform $Platform --user-id $UserId --token $Token --server $Server --webhook $Webhook --log info *>> $logPath
    $exitCode = $LASTEXITCODE

    $end = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    Add-Content -Path $logPath -Value "$end [relay-service] relay exited with code $exitCode, restart in $RestartDelaySeconds sec"
    Start-Sleep -Seconds $RestartDelaySeconds
}
