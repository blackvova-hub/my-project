param(
    [ValidateSet('start','status','logs')]
    [string]$Action = 'status'
)
$ErrorActionPreference = 'Stop'
$similarityComposeArgs = @('compose','-f',(Join-Path $PSScriptRoot 'compose.yaml'),'-f',(Join-Path $PSScriptRoot 'compose.local.yaml'))
switch ($Action) {
    'start' { & docker @similarityComposeArgs up -d --build --force-recreate backend similarity_worker frontend }
    'status' { & docker @similarityComposeArgs exec -T similarity_worker wget -q -O - http://127.0.0.1:8092/status }
    'logs' { & docker @similarityComposeArgs logs --tail 30 -f similarity_worker }
}
if ($LASTEXITCODE -ne 0) { throw "Similarity command failed: $LASTEXITCODE" }
