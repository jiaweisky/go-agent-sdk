# Derives the build version stamp. NMAKE cannot expand $(shell ...), so this
# mirrors the git logic that the POSIX Makefile evaluates inline; keep the two
# in sync.
#
# An exact tag on HEAD made of [0-9A-Za-z._+-] is a release version (a leading
# "v" before a digit is stripped: v1.2.3 -> 1.2.3, vendor-drop stays);
# otherwise the build is dev-<short-sha>; "-dirty" is appended when the working
# tree differs from HEAD (untracked files count too, because go build compiles
# them); outside git everything degrades to "dev".
#
# VERSION overrides the derived version, like `make VERSION=1.2.3`. Makefile.nmake
# also sets VERSION_SET, so `nmake VERSION=` (an empty stamp, as in
# `make VERSION=`) is told apart from an unset VERSION: NMAKE does not export
# empty macros.
#
# Usage:
#
#   versionstamp.ps1            # dev-abc1234 (dev)
#   versionstamp.ps1 version    # dev-abc1234
#   versionstamp.ps1 kind       # dev
#   versionstamp.ps1 ldflag     # -X main.version=dev-abc1234
#   versionstamp.ps1 releasecheck

[CmdletBinding()]
param(
    [ValidateSet('show', 'version', 'kind', 'ldflag', 'releasecheck')]
    [string]$What = 'show'
)

# Native stderr is expected here (git missing, no tag to describe, not a repo);
# SilentlyContinue keeps it from surfacing as an error record.
$ErrorActionPreference = 'SilentlyContinue'
$HasGit = [bool](Get-Command git -ErrorAction SilentlyContinue)

function Invoke-Git {
    param([string[]]$GitArgs)
    if (-not $HasGit) { return '' }
    $out = & git @GitArgs 2>$null
    if ($LASTEXITCODE -ne 0) { return '' }
    return (@($out) -join "`n").Trim()
}

$tag = Invoke-Git @('describe', '--tags', '--exact-match')
$tagVersion = ''
if ($tag -match '^v?[0-9A-Za-z._+-]+$') {
    $tagVersion = $tag -replace '^v([0-9])', '$1'
}

$commit = Invoke-Git @('rev-parse', '--short', 'HEAD')
$dirty = ''
if ((Invoke-Git @('status', '--porcelain')) -ne '') { $dirty = '-dirty' }

if ($tagVersion -ne '') {
    $buildVersion = "$tagVersion$dirty"
    if ($dirty) { $buildKind = 'dev' } else { $buildKind = 'release' }
} elseif ($commit -ne '') {
    $buildVersion = "dev-$commit$dirty"
    $buildKind = 'dev'
} else {
    $buildVersion = 'dev'
    $buildKind = 'dev'
}

if ($env:VERSION_SET -or $env:VERSION) {
    $buildVersion = $env:VERSION
}

# Guards for `nmake -f Makefile.nmake release`, mirroring the POSIX Makefile: a
# release archive must come from an exactly tagged, clean HEAD and carry a
# version made of [0-9A-Za-z._+-].
if ($What -eq 'releasecheck') {
    if ($tagVersion -eq '') {
        [Console]::Error.WriteLine('release: HEAD must be tagged with a version tag (e.g. v1.2.3)')
        exit 1
    }
    if ($dirty -ne '') {
        [Console]::Error.WriteLine('release: working tree is dirty, commit or stash first:')
        & git status --short | ForEach-Object { [Console]::Error.WriteLine($_) }
        exit 1
    }
    if ([string]::IsNullOrEmpty($buildVersion)) {
        [Console]::Error.WriteLine('release: VERSION is empty')
        exit 1
    }
    if ($buildVersion -notmatch '^[0-9A-Za-z._+-]+$') {
        [Console]::Error.WriteLine('release: VERSION may only contain [0-9A-Za-z._+-]')
        exit 1
    }
    exit 0
}

switch ($What) {
    'version' { $buildVersion }
    'kind' { $buildKind }
    'ldflag' { "-X main.version=$buildVersion" }
    default { "$buildVersion ($buildKind)" }
}
