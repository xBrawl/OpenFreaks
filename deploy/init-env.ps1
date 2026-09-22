param([string]$AdvertisedHost = '127.0.0.1', [string]$OutputPath = '.env')
$ErrorActionPreference = 'Stop'
if (Test-Path -LiteralPath $OutputPath) { throw 'Environment file already exists; preserve its database password and token secret.' }
if ($AdvertisedHost -notmatch '^[a-zA-Z0-9.-]+$') { throw 'Specify a hostname or IPv4 address.' }
function New-FreaksSecret {
    $bytes = New-Object byte[] 32
    $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
    return ([BitConverter]::ToString($bytes)).Replace('-', '').ToLowerInvariant()
}
$lines = @(
    'POSTGRES_DB=xbrawl', 'POSTGRES_USER=xbrawl',
    ('POSTGRES_PASSWORD=' + (New-FreaksSecret)),
    ('XBRAWL_TOKEN_SECRET=' + (New-FreaksSecret)),
    ('XBRAWL_INTERNAL_SECRET=' + (New-FreaksSecret)),
    ('XBRAWL_BATTLE_ADVERTISED_HOST=' + $AdvertisedHost),
    'XBRAWL_BATTLE_ADVERTISED_PORT=9610', 'MAIN_TCP_PORT=9609', 'BATTLE_UDP_PORT=9610',
    'BIND_HOST=0.0.0.0'
)
[IO.File]::WriteAllLines([IO.Path]::GetFullPath($OutputPath), $lines, (New-Object Text.UTF8Encoding($false)))
Write-Output "Created $OutputPath. Keep this file private and include it in your backup."
