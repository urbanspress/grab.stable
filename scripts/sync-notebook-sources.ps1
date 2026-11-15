[CmdletBinding()]
param(
    [string]$WorkspaceRoot = "C:\Development",
    [string]$OutputPath = "C:\Development\ECOSYSTEM-SNAPSHOT.md"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Invoke-GitRead {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RepoPath,
        [Parameter(Mandatory = $true)]
        [string[]]$GitArgs
    )

    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        $output = & git -C $RepoPath @GitArgs 2>$null
        $exitCode = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $previousPreference
    }

    if ($exitCode -ne 0 -or $null -eq $output) {
        return $null
    }
    return (($output -join "`n").Trim())
}

if (-not (Test-Path -LiteralPath $WorkspaceRoot -PathType Container)) {
    throw "Workspace root does not exist: $WorkspaceRoot"
}

$preferredOrder = @("GRAB", "grex-js", "grablab-cms", "grex-builder", "groxy-ai")

$discoveredDirs = Get-ChildItem -LiteralPath $WorkspaceRoot -Directory |
    Where-Object { Test-Path -LiteralPath (Join-Path $_.FullName "AGENTS.md") -PathType Leaf }

$orderedRepos = @()
foreach ($name in $preferredOrder) {
    $match = $discoveredDirs | Where-Object { $_.Name -eq $name } | Select-Object -First 1
    if ($match) {
        $orderedRepos += $match
    }
}
$additionalRepos = $discoveredDirs |
    Where-Object { $preferredOrder -notcontains $_.Name } |
    Sort-Object Name
$orderedRepos += $additionalRepos

if ($orderedRepos.Count -eq 0) {
    throw "No repositories containing AGENTS.md were found under $WorkspaceRoot"
}

$repoSummaries = New-Object System.Collections.Generic.List[object]
$sectionBlocks = New-Object System.Collections.Generic.List[string]
$totalFiles = 0

foreach ($repoDir in $orderedRepos) {
    $repoName = $repoDir.Name
    $repoPath = $repoDir.FullName

    $isGitRepo = Test-Path -LiteralPath (Join-Path $repoPath ".git")
    $branch = if ($isGitRepo) { (Invoke-GitRead -RepoPath $repoPath -GitArgs @("rev-parse", "--abbrev-ref", "HEAD")) } else { $null }
    if (-not $branch) { $branch = "(no git)" }

    $headInfo = if ($isGitRepo) { (Invoke-GitRead -RepoPath $repoPath -GitArgs @("log", "-1", "--format=%h|%cI|%s")) } else { $null }
    $headHash = "(none)"
    $headDate = "-"
    $headSubject = "-"
    if ($headInfo) {
        $parts = $headInfo.Split("|", 3)
        if ($parts.Count -ge 1 -and $parts[0]) { $headHash = $parts[0] }
        if ($parts.Count -ge 2 -and $parts[1]) { $headDate = $parts[1] }
        if ($parts.Count -ge 3 -and $parts[2]) { $headSubject = $parts[2] }
    }

    $latestTag = if ($isGitRepo) { (Invoke-GitRead -RepoPath $repoPath -GitArgs @("describe", "--tags", "--abbrev=0")) } else { $null }
    if (-not $latestTag) {
        $allTagsRaw = if ($isGitRepo) { (Invoke-GitRead -RepoPath $repoPath -GitArgs @("tag", "--sort=-v:refname")) } else { $null }
        if ($allTagsRaw) {
            $latestTag = ($allTagsRaw -split "`n")[0].Trim()
        } else {
            $latestTag = "(none)"
        }
    }

    $tagListRaw = if ($isGitRepo) { (Invoke-GitRead -RepoPath $repoPath -GitArgs @("tag", "--sort=-v:refname")) } else { $null }
    $tagSummary = if ($tagListRaw) {
        $tagItems = @($tagListRaw -split "`n" | ForEach-Object { $_.Trim() } | Where-Object { $_ })
        if ($tagItems.Count -gt 5) {
            (($tagItems | Select-Object -First 5) -join ", ") + " (+" + ($tagItems.Count - 5) + " more)"
        } else {
            ($tagItems -join ", ")
        }
    } else {
        "(none)"
    }

    $originMain = if ($isGitRepo) { (Invoke-GitRead -RepoPath $repoPath -GitArgs @("rev-parse", "--short", "origin/main")) } else { $null }
    if (-not $originMain) { $originMain = "-" }

    $stableMain = if ($isGitRepo) { (Invoke-GitRead -RepoPath $repoPath -GitArgs @("rev-parse", "--short", "stable/main")) } else { $null }
    if (-not $stableMain) { $stableMain = "-" }

    $porcelain = if ($isGitRepo) { (Invoke-GitRead -RepoPath $repoPath -GitArgs @("status", "--porcelain")) } else { $null }
    $treeState = if (-not $isGitRepo) { "n/a" } elseif ([string]::IsNullOrWhiteSpace($porcelain)) { "clean" } else { "dirty" }

    $filesToMerge = New-Object System.Collections.Generic.List[System.IO.FileInfo]

    $agentsFile = Get-Item -LiteralPath (Join-Path $repoPath "AGENTS.md")
    $filesToMerge.Add($agentsFile)

    $expertFiles = Get-ChildItem -LiteralPath $repoPath -Filter "*-EXPERT.md" -File | Sort-Object Name
    foreach ($f in $expertFiles) {
        $filesToMerge.Add($f)
    }

    $handoffPath = Join-Path $repoPath "HANDOFF.md"
    if (Test-Path -LiteralPath $handoffPath -PathType Leaf) {
        $filesToMerge.Add((Get-Item -LiteralPath $handoffPath))
    }

    $roadmapPath = Join-Path $repoPath "docs\ROADMAP.md"
    if (Test-Path -LiteralPath $roadmapPath -PathType Leaf) {
        $filesToMerge.Add((Get-Item -LiteralPath $roadmapPath))
    }

    $specsDir = Join-Path $repoPath "specifications"
    if (Test-Path -LiteralPath $specsDir -PathType Container) {
        $specFiles = Get-ChildItem -LiteralPath $specsDir -Filter "*.md" -File | Sort-Object Name
        foreach ($f in $specFiles) {
            $filesToMerge.Add($f)
        }
    }

    $repoSummaries.Add([PSCustomObject]@{
        Repo        = $repoName
        Branch      = $branch
        HeadHash    = $headHash
        HeadDate    = $headDate
        HeadSubject = ($headSubject -replace '\|', '\|')
        LatestTag   = $latestTag
        Tags        = $tagSummary
        OriginMain  = $originMain
        StableMain  = $stableMain
        TreeState   = $treeState
        FileCount   = $filesToMerge.Count
    })

    foreach ($fileInfo in $filesToMerge) {
        $relativePath = $fileInfo.FullName.Substring($repoPath.Length).TrimStart('\', '/').Replace('\', '/')
        $rawText = [System.IO.File]::ReadAllText($fileInfo.FullName, [System.Text.Encoding]::UTF8)
        $normalizedText = ($rawText -replace "`r`n", "`n").TrimEnd("`r", "`n")
        $section = @(
            "---",
            "",
            "# [$repoName] — $relativePath",
            "",
            $normalizedText
        ) -join "`n"
        $sectionBlocks.Add($section)
        $totalFiles++
    }
}

$utcNow = [DateTime]::UtcNow.ToString("yyyy-MM-dd HH:mm:ss 'UTC'", [System.Globalization.CultureInfo]::InvariantCulture)
$localNow = [DateTime]::Now.ToString("yyyy-MM-dd HH:mm:ss zzz", [System.Globalization.CultureInfo]::InvariantCulture)

$headerLines = New-Object System.Collections.Generic.List[string]
$headerLines.Add("# GRAB Ecosystem Snapshot")
$headerLines.Add("")
$headerLines.Add("- **Generated At:** $utcNow ($localNow)")
$headerLines.Add("- **Workspace Root:** ``$WorkspaceRoot``")
$headerLines.Add("- **Generator:** ``C:\Development\GRAB\scripts\sync-notebook-sources.ps1``")
$headerLines.Add("- **Discovered Repositories ($($repoSummaries.Count)):** " + (($repoSummaries | ForEach-Object { "``$($_.Repo)``" }) -join ", "))
$headerLines.Add("- **Total Merged Documents:** $totalFiles")
$headerLines.Add("")
$headerLines.Add("## Live Git Branch, Commit & Tag Summary")
$headerLines.Add("")
$headerLines.Add("| Repository | Branch | HEAD | origin/main | stable/main | Latest Tag | Working Tree | Merged Docs | Latest Commit |")
$headerLines.Add("|---|---|---|---|---|---|---|---|---|")
foreach ($s in $repoSummaries) {
    $headerLines.Add("| ``$($s.Repo)`` | ``$($s.Branch)`` | ``$($s.HeadHash)`` | ``$($s.OriginMain)`` | ``$($s.StableMain)`` | ``$($s.LatestTag)`` | ``$($s.TreeState)`` | $($s.FileCount) | $($s.HeadSubject) |")
}
$headerLines.Add("")
$headerLines.Add("### Repository Tag Inventory")
$headerLines.Add("")
foreach ($s in $repoSummaries) {
    $headerLines.Add("- **$($s.Repo)** (``$($s.HeadDate)``): Tags: $($s.Tags)")
}

$finalContent = ($headerLines -join "`n") + "`n`n" + ($sectionBlocks -join "`n`n") + "`n"

$outputDir = Split-Path -Parent $OutputPath
if ($outputDir -and -not (Test-Path -LiteralPath $outputDir -PathType Container)) {
    New-Item -ItemType Directory -Path $outputDir -Force | Out-Null
}

$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($OutputPath, $finalContent, $utf8NoBom)

$writtenInfo = Get-Item -LiteralPath $OutputPath
Write-Host ("Generated {0} ({1:N0} bytes) from {2} repositories and {3} documents." -f $OutputPath, $writtenInfo.Length, $repoSummaries.Count, $totalFiles)
