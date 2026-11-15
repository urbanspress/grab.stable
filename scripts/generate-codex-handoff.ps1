[CmdletBinding()]
param(
    [string]$WorkspaceRoot = "C:\Development",
    [string]$OutputPath = "C:\Development\CODEX-HANDOFF.md"
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

function Read-Utf8Trimmed {
    param([string]$FilePath)
    if (-not (Test-Path -LiteralPath $FilePath -PathType Leaf)) {
        return $null
    }
    $raw = [System.IO.File]::ReadAllText($FilePath, [System.Text.Encoding]::UTF8)
    return (($raw -replace "`r`n", "`n").TrimEnd("`r", "`n"))
}

function Extract-MarkdownField {
    param(
        [string]$Content,
        [string]$FieldLabel
    )
    if ([string]::IsNullOrWhiteSpace($Content)) {
        return "-"
    }
    $escaped = [Regex]::Escape($FieldLabel)
    $pattern = "(?m)^-\s+\*\*$escaped\s*:\*\*\s*(.+)$"
    $m = [Regex]::Match($Content, $pattern)
    if ($m.Success) {
        return $m.Groups[1].Value.Trim()
    }
    return "-"
}

if (-not (Test-Path -LiteralPath $WorkspaceRoot -PathType Container)) {
    throw "Workspace root does not exist: $WorkspaceRoot"
}

$canonicalOrder = @(
    "GRAB",
    "grex-js",
    "grablab-cms",
    "grex-builder",
    "groxy-ai",
    "grabman",
    "grabtaculous",
    "gashboard",
    "groxy-desk"
)

$discoveredDirs = Get-ChildItem -LiteralPath $WorkspaceRoot -Directory |
    Where-Object { Test-Path -LiteralPath (Join-Path $_.FullName "AGENTS.md") -PathType Leaf }

$orderedRepos = @()
foreach ($name in $canonicalOrder) {
    $match = $discoveredDirs | Where-Object { $_.Name -eq $name } | Select-Object -First 1
    if ($match) {
        $orderedRepos += $match
    }
}
$additionalRepos = $discoveredDirs |
    Where-Object { $canonicalOrder -notcontains $_.Name } |
    Sort-Object Name
$orderedRepos += $additionalRepos

if ($orderedRepos.Count -eq 0) {
    throw "No repositories containing AGENTS.md were found under $WorkspaceRoot"
}

$repoRows = New-Object System.Collections.Generic.List[object]
$repoDetails = New-Object System.Collections.Generic.List[string]

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

    $originMain = if ($isGitRepo) { (Invoke-GitRead -RepoPath $repoPath -GitArgs @("rev-parse", "--short", "origin/main")) } else { $null }
    if (-not $originMain) { $originMain = "-" }

    $stableMain = if ($isGitRepo) { (Invoke-GitRead -RepoPath $repoPath -GitArgs @("rev-parse", "--short", "stable/main")) } else { $null }
    if (-not $stableMain) { $stableMain = "-" }

    $syncState = if ($originMain -ne "-" -and $stableMain -ne "-") {
        if ($originMain -eq $stableMain) { "in-sync (.progress == .stable)" } else { "ahead on .progress ($originMain vs $stableMain)" }
    } else {
        "n/a"
    }

    $porcelain = if ($isGitRepo) { (Invoke-GitRead -RepoPath $repoPath -GitArgs @("status", "--porcelain")) } else { $null }
    $treeState = if (-not $isGitRepo) { "n/a" } elseif ([string]::IsNullOrWhiteSpace($porcelain)) { "clean" } else { "dirty" }

    $handoffContent = Read-Utf8Trimmed -FilePath (Join-Path $repoPath "HANDOFF.md")
    $roadmapContent = Read-Utf8Trimmed -FilePath (Join-Path $repoPath "docs\ROADMAP.md")

    $owner = Extract-MarkdownField -Content $handoffContent -FieldLabel "Current Owner"
    $milestone = Extract-MarkdownField -Content $handoffContent -FieldLabel "Current Milestone"

    $repoRows.Add([PSCustomObject]@{
        Repo        = $repoName
        Branch      = $branch
        HeadHash    = $headHash
        HeadDate    = $headDate
        HeadSubject = ($headSubject -replace '\|', '\|')
        LatestTag   = $latestTag
        OriginMain  = $originMain
        StableMain  = $stableMain
        SyncState   = $syncState
        TreeState   = $treeState
        Owner       = ($owner -replace '\|', '\|')
        Milestone   = ($milestone -replace '\|', '\|')
    })

    $detailLines = New-Object System.Collections.Generic.List[string]
    $detailLines.Add("### ``$repoName`` (``$repoPath``)")
    $detailLines.Add("- **Branch / HEAD:** ``$branch`` @ ``$headHash`` ($headDate) — $headSubject")
    $detailLines.Add("- **Latest Release Tag:** ``$latestTag`` | **origin/main (``.progress``):** ``$originMain`` | **stable/main (``.stable``):** ``$stableMain`` ($syncState)")
    $detailLines.Add("- **Working Tree:** ``$treeState``")
    if ($handoffContent) {
        $detailLines.Add("")
        $detailLines.Add("#### Live ``HANDOFF.md`` (``$repoName``)")
        $detailLines.Add('```markdown')
        $detailLines.Add($handoffContent)
        $detailLines.Add('```')
    }
    if ($roadmapContent) {
        $detailLines.Add("")
        $detailLines.Add("#### Live ``docs/ROADMAP.md`` (``$repoName``)")
        $detailLines.Add('```markdown')
        $detailLines.Add($roadmapContent)
        $detailLines.Add('```')
    }
    $repoDetails.Add(($detailLines -join "`n"))
}

$activeRepo = $repoRows | Where-Object { $_.Repo -eq "groxy-desk" } | Select-Object -First 1
if (-not $activeRepo) {
    $activeRepo = $repoRows | Select-Object -Last 1
}

$activeHandoff = Read-Utf8Trimmed -FilePath (Join-Path $WorkspaceRoot "$($activeRepo.Repo)\HANDOFF.md")
$activeRoadmap = Read-Utf8Trimmed -FilePath (Join-Path $WorkspaceRoot "$($activeRepo.Repo)\docs\ROADMAP.md")

$utcNow = [DateTime]::UtcNow.ToString("yyyy-MM-dd HH:mm:ss 'UTC'", [System.Globalization.CultureInfo]::InvariantCulture)
$localNow = [DateTime]::Now.ToString("yyyy-MM-dd HH:mm:ss zzz", [System.Globalization.CultureInfo]::InvariantCulture)

$lines = New-Object System.Collections.Generic.List[string]
$lines.Add("# CODEX-HANDOFF.md — Zero-Hiccup Cross-Agent Context Briefing (`Google Antigravity <-> OpenAI Codex`)")
$lines.Add("")
$lines.Add("- **Generated At:** $utcNow ($localNow)")
$lines.Add("- **Workspace Root:** ``$WorkspaceRoot``")
$lines.Add("- **Generator:** ``C:\Development\GRAB\scripts\generate-codex-handoff.ps1``")
$lines.Add("- **Companion Snapshot:** ``C:\Development\ECOSYSTEM-SNAPSHOT.md`` (via ``C:\Development\GRAB\scripts\sync-notebook-sources.ps1``)")
$lines.Add("- **Discovered Twin Repositories ($($repoRows.Count)):** " + (($repoRows | ForEach-Object { "``$($_.Repo)``" }) -join ", "))
$lines.Add("")
$lines.Add("---")
$lines.Add("")
$lines.Add("## 1. Live 9-Repository Status & Version Matrix (``.progress`` vs ``.stable``)")
$lines.Add("")
$lines.Add("| # | Repository | Latest Tag | HEAD | ``origin/main`` (``.progress``) | ``stable/main`` (``.stable``) | Twin Sync Status | Tree | Current Milestone (`HANDOFF.md`) |")
$lines.Add("|---|---|---|---|---|---|---|---|---|")

$idx = 1
foreach ($r in $repoRows) {
    $lines.Add("| $idx | ``$($r.Repo)`` | ``$($r.LatestTag)`` | ``$($r.HeadHash)`` | ``$($r.OriginMain)`` | ``$($r.StableMain)`` | $($r.SyncState) | ``$($r.TreeState)`` | $($r.Milestone) |")
    $idx++
}

$lines.Add("")
$lines.Add("### Frozen Upstream Baselines (Steps 0–5) vs Active Step 6 (`groxy-desk`)")
$lines.Add("- **Step 0 — Core Language, Bytecode VM & Grisp Server (`C:\Development\GRAB`):** Frozen at **`v1.1.0`** (in-sync across `urbanspress/grab.progress` & `urbanspress/grab.stable`).")
$lines.Add("- **Step 1 — Reactive Frontend Runtime & 20-Element Library (`C:\Development\grex-js`):** Frozen at **`v1.0.0`** (in-sync across `urbanspress/grex-js.progress` & `urbanspress/grex-js.stable`).")
$lines.Add("- **Step 2 — Flagship Zero-Plugin CMS (`C:\Development\grablab-cms`):** Frozen at **`v1.0.1`** (in-sync across `urbanspress/grablab-cms.progress` & `urbanspress/grablab-cms.stable`).")
$lines.Add("- **Step 3 — Exclusive `mode: ""visual""` Theme & Studio (`C:\Development\grex-builder`):** Frozen at **`v1.0.0`** (in-sync across `urbanspress/grex-builder.progress` & `urbanspress/grex-builder.stable`).")
$lines.Add("- **Step 4 — Autonomous Design, Content & SEO Agent (`C:\Development\groxy-ai`):** Frozen at **`v1.0.0`** (in-sync across `urbanspress/groxy-ai.progress` & `urbanspress/groxy-ai.stable`).")
$lines.Add("- **Step 5 — Scaffolder, Orchestrator & Discreet VPS Panel (`grabman`, `grabtaculous`, `gashboard`):** Frozen at **`v1.0.0`** (in-sync across `.progress` & `.stable`).")
$lines.Add("- **Step 6 — Native Desktop Vibe-Coding IDE & Studio (`C:\Development\groxy-desk`):** Active development repository (`urbanspress/groxy-desk.progress` / `urbanspress/groxy-desk.stable`). Never modify frozen Step 0–5 repositories unless explicitly instructed by the project owner.")
$lines.Add("")
$lines.Add("---")
$lines.Add("")
$lines.Add("## 2. Non-Negotiable Ecosystem Invariants (Never-Break Rules)")
$lines.Add("")
$lines.Add("1. **Pure `Grex JS v1.0.0` Zero-`<div>` & Zero-XSS DOM (`GREX-SCHEMA-001`):**")
$lines.Add("   - Every `.grab.html` template and JS UI module MUST use strictly the 20 semantic Grex elements (`Section`, `Container`, `Grid`, `Stack`, `Card`, `Heading`, `Text`, `RichText`, `Link`, `Button`, `Image`, `Icon`, `Form`, `Input`, `Select`, `Tabs`, `Accordion`, `Modal`, `Nav`, `ForEach`) and semantic HTML5 tags.")
$lines.Add("   - Enforce **`divCount === 0`**, **`innerHTMLWriteCount === 0`**, zero `eval()` / `new Function()`, and zero unsafe URL protocols (`javascript:`, `vbscript:`, `file:`, unsanitized `data:`).")
$lines.Add("   - Enforce **100% WCAG AA contrast (`>= 4.5:1` normal text, `>= 3.0:1` large text)** across both Light and Dark themes (`data-grex-theme=""light|dark""`) with zero FOUC and `100%` `auditAccessibility()` pass.")
$lines.Add("2. **Single Unified Default Agent (`Groxy AI` Only — Zero Model-Switching Dropdowns):**")
$lines.Add("   - **Groxy AI** is the sole permanent built-in agent identity in `groxy-desk`. Never render model-picker `<select>` or provider-switching dropdowns in the UI; route automatically via the **Automatic Adaptive Reasoning Router** (Instant/Fast tier vs Deep/High-Reasoning tier) with local-first Ollama/OpenCode support and zero mandatory cloud API fees.")
$lines.Add("3. **Exact Base-10 Numeric Fidelity (`ADR-025c`):**")
$lines.Add("   - Preserve exact base-10 `GrabDecimal` and `GrabInteger` wire strings across all JSON payloads, `Site DNA` (`groxy-site-dna-v0.4.0`), custom fields, `schema.org` prices, ports, and Visual DB Explorer cells without IEEE-754 floating-point drift.")
$lines.Add("4. **Human-in-the-Loop Safety (`GROXY-SAFE-001`) & Draft-Only CMS Enforcement:**")
$lines.Add("   - Every mutating `POST` request requires constant-time `Authorization: Bearer <token>` + `X-CSRF-Token` (`403 DESK-E403`).")
$lines.Add("   - Every AI file write, layout apply, DB write/DDL/import, DNS change, or fleet rollout requires a **single-use, target-bound `approval_token`** (`403 DESK-E403` on mismatch, `409 DESK-E409` on replay) plus an automatic SHA-256 **Pre-Mutation / Pre-Deploy Snapshot** with 1-click rollback.")
$lines.Add("   - GrabLab CMS `create_entry_draft` MUST strictly enforce `status_enforced: ""draft""`.")
$lines.Add("5. **Loopback-Only Binding (`ADR-005`), Structured `argv` (`ADR-017`) & `GRAB-E401` Confinement:**")
$lines.Add("   - Bind local daemons (`groxy-deskd` `127.0.0.1:9490`, `Grisp` workers `127.0.0.1:8765–8772`, SSH DB tunnels `127.0.0.1:15432–15439`, `grabtaculousd` `127.0.0.1:9440`) strictly to `127.0.0.1`.")
$lines.Add("   - Spawn subprocesses exclusively via structured OS `argv` slices (`exec.CommandContext`) — NEVER `sh -c`, `cmd /c`, or shell interpolation. Enforce `known_hosts` `SHA256:...` host-key pinning on all SSH connections (zero `InsecureIgnoreHostKey`).")
$lines.Add("   - Enforce `GRAB-E401` workspace path confinement (reject `\x00`, UNC `\\`/`//`, `..` traversal, escaping symlinks, and `.git`, `.env`, `.ssh`, `id_rsa`, `id_ed25519`, `*.pem`, `*.key`, `*.p12`, `*.pfx`).")
$lines.Add("6. **Hardened `--read-only` Container Isolation (`Grabtaculous`):**")
$lines.Add("   - All orchestrated fleet containers enforce `read_only: true`, `cap_drop: [""ALL""]`, `security_opt: [""no-new-privileges:true""]`, non-root `65532:65532`, pinned digests (no `:latest`), and `grabman solve --verify` preflight.")
$lines.Add("7. **Dual-Repo Protection (`origin` vs `stable`), SemVer & 7-Layer Compatibility Matrix:**")
$lines.Add("   - Push work-in-progress commits and unapproved specs ONLY to `origin` (`.progress`). Never push to `stable` (`.stable`) until a milestone is 100% `Complete`, all 4 verification gates pass 100%, Inno Setup `.exe` installer (never `.msi`) + walkthrough PDF + `SHA256SUMS.txt` are built, and the **7-Layer Ecosystem Compatibility Matrix** is included.")
$lines.Add("8. **Mandatory Post-Milestone Snapshot & Codex Handoff Sync Hooks:**")
$lines.Add("   - At the end of every milestone or handoff checkpoint, always run both:")
$lines.Add('     ```powershell')
$lines.Add('     powershell -ExecutionPolicy Bypass -File C:\Development\GRAB\scripts\sync-notebook-sources.ps1')
$lines.Add('     powershell -ExecutionPolicy Bypass -File C:\Development\GRAB\scripts\generate-codex-handoff.ps1')
$lines.Add('     ```')
$lines.Add("")
$lines.Add("---")
$lines.Add("")
$lines.Add("## 3. Active Focus: ``$($activeRepo.Repo)`` (`C:\Development\$($activeRepo.Repo)`)")
$lines.Add("")
$lines.Add("- **Active Repository:** ``C:\Development\$($activeRepo.Repo)``")
$lines.Add("- **Current Owner:** $($activeRepo.Owner)")
$lines.Add("- **Current Milestone:** $($activeRepo.Milestone)")
$lines.Add("- **Git State:** Branch ``$($activeRepo.Branch)`` @ ``$($activeRepo.HeadHash)`` | ``origin/main``: ``$($activeRepo.OriginMain)`` | ``stable/main``: ``$($activeRepo.StableMain)`` ($($activeRepo.SyncState))")
$lines.Add("")
if ($activeHandoff) {
    $lines.Add("### Active ``HANDOFF.md`` (`C:\Development\$($activeRepo.Repo)\HANDOFF.md`)")
    $lines.Add("")
    $lines.Add('```markdown')
    $lines.Add($activeHandoff)
    $lines.Add('```')
    $lines.Add("")
}
if ($activeRoadmap) {
    $lines.Add("### Active ``docs/ROADMAP.md`` (`C:\Development\$($activeRepo.Repo)\docs\ROADMAP.md`)")
    $lines.Add("")
    $lines.Add('```markdown')
    $lines.Add($activeRoadmap)
    $lines.Add('```')
    $lines.Add("")
}
$lines.Add("### Immediate Pickup Checklist for OpenAI Codex (`use-codex`) or Google Antigravity (`use-antigravity`)")
$lines.Add("1. Read `C:\Development\groxy-desk\GROXY-DESK-EXPERT.md`, `C:\Development\groxy-desk\AGENTS.md`, `C:\Development\groxy-desk\HANDOFF.md`, and the active specification in `C:\Development\groxy-desk\specifications\`.")
$lines.Add("2. Respect the **Spec-First Approval Gate**: do NOT write application code for an unapproved specification until the project owner explicitly approves it.")
$lines.Add("3. Once approved for implementation, verify all 4 mandatory gates before marking complete:")
$lines.Add("   - **Gate 1 (Go):** `go build ./...`, `go vet ./...`, `go test -count=1 ./...`, `govulncheck ./...` (`0` vulnerabilities).")
$lines.Add("   - **Gate 2 (GRAB & Grex DOM):** `grab check`, `grab format`, and `node --test tests/*.test.js` (`divCount === 0`, `innerHTMLWriteCount === 0`, WCAG AA `>= 4.5:1` Light & Dark, `ADR-025c`).")
$lines.Add("   - **Gate 3 (Security & Confinement):** `DESK-E501` loopback bind, `GRAB-E401` path confinement, `GROXY-SAFE-001` `403`/`409` approval token & snapshot rollback, `known_hosts` SSH pinning.")
$lines.Add("   - **Gate 4 (Live Cross-Layer E2E):** `python tests/integration_e2e.py` passing `100%` while all 8 peer `v1.x` repositories remain untouched and clean.")
$lines.Add("")
$lines.Add("---")
$lines.Add("")
$lines.Add("## 4. Per-Repository Live Handoff & Roadmap Reference (All 9 Repositories)")
$lines.Add("")
$lines.Add(($repoDetails -join "`n`n---`n`n"))
$lines.Add("")

$finalContent = ($lines -join "`n")

$outputDir = Split-Path -Parent $OutputPath
if ($outputDir -and -not (Test-Path -LiteralPath $outputDir -PathType Container)) {
    New-Item -ItemType Directory -Path $outputDir -Force | Out-Null
}

$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($OutputPath, $finalContent, $utf8NoBom)

$writtenInfo = Get-Item -LiteralPath $OutputPath
Write-Host ("Generated {0} ({1:N0} bytes) across {2} repositories." -f $OutputPath, $writtenInfo.Length, $repoRows.Count)
