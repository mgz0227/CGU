[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidatePattern('^v\d+\.\d+\.\d+$')]
    [string] $Tag,

    [Parameter(ParameterSetName = 'File')]
    [string] $NotesFile,

    [Parameter(ParameterSetName = 'Text')]
    [string] $Notes,

    [string] $Title,
    [string] $Repository = 'mgz0227/CGU',
    [switch] $Edit,
    [switch] $ValidateOnly
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($NotesFile) -and [string]::IsNullOrWhiteSpace($Notes)) {
    throw 'Provide either -NotesFile or -Notes.'
}
if (-not [string]::IsNullOrWhiteSpace($NotesFile) -and -not [string]::IsNullOrWhiteSpace($Notes)) {
    throw 'Use only one of -NotesFile and -Notes.'
}

if ([string]::IsNullOrWhiteSpace($Title)) {
    $Title = "CGU $Tag"
}
if ($Title -match '[\r\n]') {
    throw 'Release title must not contain a newline.'
}

$utf8 = [System.Text.UTF8Encoding]::new($false)
if (-not [string]::IsNullOrWhiteSpace($NotesFile)) {
    $resolvedNotesFile = (Resolve-Path -LiteralPath $NotesFile -ErrorAction Stop).Path
    $body = [System.IO.File]::ReadAllText($resolvedNotesFile, $utf8)
} else {
    $body = $Notes
}

# GitHub accepts either LF or CRLF, but using one canonical form makes the
# local and remote verification deterministic across Windows and Linux.
$body = $body -replace "`r`n?", "`n"
$body = $body.Trim()
if ([string]::IsNullOrWhiteSpace($body)) {
    throw 'Release notes must not be empty.'
}

# A literal "\n" is what caused the broken one-paragraph release page. It is
# almost always an escaped JSON/string value accidentally passed to gh.
if ($body -match '\\n' -or $body -match '\\r') {
    throw 'Release notes contain a literal \\n or \\r sequence; use real line breaks.'
}
if (-not $body.Contains("`n")) {
    throw 'Release notes must contain real line breaks; use --notes-file or a PowerShell here-string.'
}
if (-not ($body -split "`n" | Where-Object { $_ -match '^\s{0,3}#{1,6}\s+\S' })) {
    throw 'Release notes must contain at least one Markdown heading.'
}

$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("cgu-release-" + [guid]::NewGuid().ToString('N'))
$notesPath = Join-Path $tempRoot 'notes.md'
try {
    New-Item -ItemType Directory -Path $tempRoot -Force | Out-Null
    [System.IO.File]::WriteAllText($notesPath, $body, $utf8)

    if ($ValidateOnly) {
        Write-Output ("Validated {0}: {1} characters, {2} lines." -f $Tag, $body.Length, ($body -split "`n").Count)
        return
    }

    $operation = if ($Edit) { 'edit' } else { 'create' }
    $ghArgs = @(
        'release', $operation, $Tag,
        '--repo', $Repository,
        '--title', $Title,
        '--notes-file', $notesPath
    )
    if (-not $Edit) {
        # Do not silently create a release from a typo or an unpushed tag.
        $ghArgs += '--verify-tag'
    }

    & gh @ghArgs
    if ($LASTEXITCODE -ne 0) {
        throw "gh release $operation failed with exit code $LASTEXITCODE."
    }

    $remoteBody = @(& gh release view $Tag --repo $Repository --json body --jq .body) -join "`n"
    if ($LASTEXITCODE -ne 0) {
        throw 'The release was created, but its remote body could not be read back.'
    }
    $remoteBody = ($remoteBody -replace "`r`n?", "`n").Trim()
    if ($remoteBody -match '\\n' -or $remoteBody -match '\\r') {
        throw 'Remote release body still contains a literal escaped newline.'
    }
    if ($remoteBody -cne $body) {
        throw 'Remote release body differs from the validated local Markdown.'
    }
    Write-Output ("Release {0} verified: multiline Markdown body matches the local notes." -f $Tag)
} finally {
    if (Test-Path -LiteralPath $tempRoot) {
        Remove-Item -LiteralPath $tempRoot -Recurse -Force -ErrorAction SilentlyContinue
    }
}
