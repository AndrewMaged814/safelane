# Risky Studio demo

Copy the unresolved assessment into a disposable workspace, then start the one-process Studio:

```powershell
$studioWorkspace = Join-Path $env:TEMP ("safelane-studio-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $studioWorkspace | Out-Null
Copy-Item demo/studio-risky/assessment.json (Join-Path $studioWorkspace "assessment.json")
uv run safelane studio --workspace $studioWorkspace
```

Open <http://127.0.0.1:4173>. Approve Strict once and observe `Resolved` plus the absolute
`decision.json` path. Replaying the stale request returns HTTP 409 and writes nothing. Approval only
records the rollout plan; it does not release software.
