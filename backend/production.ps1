param(
    [ValidateSet('start', 'build', 'stop', 'status', 'logs')]
    [string]$Action = 'start'
)

$ErrorActionPreference = 'Stop'
$projectDir = $PSScriptRoot
$composeArgs = @(
    'compose',
    '-f', (Join-Path $projectDir 'compose.yaml'),
    '-f', (Join-Path $projectDir 'compose.local.yaml')
)

function Test-DockerEngine {
    $savedPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & docker info *> $null
    $ready = $LASTEXITCODE -eq 0
    $ErrorActionPreference = $savedPreference
    return $ready
}

function Wait-DockerEngine {
    if (Test-DockerEngine) {
        return
    }

    $desktop = 'C:\Program Files\Docker\Docker\Docker Desktop.exe'
    if (-not (Test-Path -LiteralPath $desktop)) {
        throw "Docker Desktop not found at $desktop"
    }

    Start-Process -FilePath $desktop -WindowStyle Hidden
    for ($attempt = 0; $attempt -lt 90; $attempt++) {
        Start-Sleep -Seconds 2
        if (Test-DockerEngine) {
            return
        }
    }

    throw 'Docker Desktop did not become ready within 3 minutes.'
}

Set-Location -LiteralPath $projectDir

switch ($Action) {
    'start' {
        Wait-DockerEngine
        & docker @composeArgs up -d --remove-orphans
    }
    'build' {
        Wait-DockerEngine
        & docker @composeArgs up -d --build --force-recreate --remove-orphans
    }
    'stop' {
        Wait-DockerEngine
        & docker @composeArgs stop
    }
    'status' {
        Wait-DockerEngine
        & docker @composeArgs ps
    }
    'logs' {
        Wait-DockerEngine
        & docker @composeArgs logs --tail 200 -f
    }
}

if ($LASTEXITCODE -ne 0) {
    throw "Docker command failed with exit code $LASTEXITCODE"
}
