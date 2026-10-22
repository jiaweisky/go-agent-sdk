# Helpers for `nmake -f Makefile.nmake release`. They live in a script because a
# recipe line cannot hold these snippets inline: NMAKE expands `$` as a macro, so
# every PowerShell variable would need `$$` escaping.
#
#   release-dist.ps1 rmtree <path>     remove a directory, retrying briefly: a
#                                      freshly written binary can still be locked
#                                      by a scanner or indexer
#   release-dist.ps1 sha256sums <dir>  write <dir>\sha256sums.txt in the same
#                                      "<hash>  <name>" format as sha256sum

[CmdletBinding()]
param(
    [Parameter(Mandatory, Position = 0)]
    [ValidateSet('rmtree', 'sha256sums')]
    [string]$What,
    [Parameter(Mandatory, Position = 1)]
    [string]$Path
)

switch ($What) {
    'rmtree' {
        for ($i = 0; $i -lt 30; $i++) {
            try {
                Remove-Item -LiteralPath $Path -Recurse -Force -ErrorAction Stop
                exit 0
            } catch {
                Start-Sleep -Milliseconds 200
            }
        }
        [Console]::Error.WriteLine("release: cannot remove $Path")
        exit 1
    }
    'sha256sums' {
        Get-ChildItem -LiteralPath $Path -File |
            Where-Object { $_.Name -ne 'sha256sums.txt' } |
            Sort-Object Name |
            ForEach-Object {
                '{0}  {1}' -f (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLower(), $_.Name
            } |
            Set-Content -LiteralPath (Join-Path $Path 'sha256sums.txt') -Encoding ascii
    }
}
