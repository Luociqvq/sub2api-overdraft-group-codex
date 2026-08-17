---
name: sub2api-custom-deployer
description: "Restore, deploy, inspect, or adapt three Sub2API custom features after upstream updates: 破甲分组/group-level Codex instructions, Codex 5h/7d quota overdraft, and bounded fresh-connection retries for selected upstream 429 responses. Use when the user asks to update Sub2API without losing customizations, quickly reapply the three patches, verify whether all three features are present, or adapt them to a newer upstream version."
---

# Sub2API Custom Deployer

This skill maintains one ordered customization bundle:

1. **额度透支服务** — Codex 5h/7d quota overdraft request injection, probe coordination, scheduling gates, usage display, and fork-safe update behavior.
2. **破甲分组** — group-level Codex instruction fields, database migration, backend injection, API mapping, and admin UI controls.
3. **429 上游断连重连** — for eligible no-reset or explicit 5h responses, close the old upstream response, rebuild the selected connection pool, and replay on the same account at most twice. Explicit 7d exhaustion and `Retry-After` retain the existing cooldown path.

The versioned patch assets live under `assets/patches/` and must be applied in filename order. `assets/feature-manifest.json` is the source of truth for feature markers and tested upstream baseline.

## Standard Reapply Workflow

When the user asks to reapply the bundle after updating Sub2API:

1. Resolve the target Git repository. Treat a user-provided path as authoritative. If no path is given, inspect the current workspace for the Sub2API repository instead of guessing.
2. Inspect `git status`, current branch, upstream version, and recent commits. Preserve all existing work. The deployment script requires a clean worktree; do not stash, discard, reset, or commit user changes without explicit direction.
3. Run the deployment script in check mode first:

   ```powershell
   pwsh -File scripts/Deploy-Sub2ApiCustom.ps1 -RepoPath TARGET -CheckOnly
   ```

4. If preflight succeeds, apply on a new branch and run focused verification:

   ```powershell
   pwsh -File scripts/Deploy-Sub2ApiCustom.ps1 -RepoPath TARGET -RunTests
   ```

   The script creates a timestamped `custom/sub2api-three-features-*` branch unless `-NoBranch` is explicitly requested. It never commits or deploys the running service.
5. Confirm the actual runtime config—not only `deploy/config.example.yaml`—contains:

   ```yaml
   gateway:
     codex_quota_overdraft_enabled: true
   ```

6. Review `git diff`, run any deployment-specific build, and report the branch, applied/skipped patches, tests, migration presence, and remaining runtime actions.

## Newer-Upstream Conflict Workflow

The bundled patches are tested against Sub2API `v0.1.177`. A later upstream may move integration points. When preflight reports a conflict:

1. Do not force the patch, copy whole old files over new files, or bypass the clean-worktree guard.
2. Read `assets/feature-manifest.json`, the relevant patch hunks, and `assets/docs/overdraft-customization.md`.
3. Reapply behavior by feature boundary, preserving current upstream code:
   - restore overdraft configuration, request-context marker, injection, coordinator wiring, scheduler gates, usage persistence/UI, and fork update guards;
   - restore `codex_instructions_enabled` / `codex_instructions`, migration `224_group_codex_instructions.sql`, DTO/repository/cache mapping, all supported request-path injection points, and admin UI;
   - restore the optional `HTTPUpstreamConnectionResetter`, scoped transport eviction, bounded 429 eligibility guards, and the `Forward` retry loop.
4. Regenerate Ent and Wire only when their schemas/wiring changed. Prefer repository generation commands already documented by the target version.
5. Run `scripts/Test-Sub2ApiCustom.ps1 -RepoPath TARGET -RunTests -Frontend` and address failures. Do not declare success based only on marker checks.
6. Produce an updated compatibility patch for the new upstream version only after tests pass. Keep the three logical layers separate so future conflicts remain diagnosable.

## Inspection-Only Workflow

When the user asks whether the features are still present, do not mutate the repository. Run:

```powershell
pwsh -File scripts/Test-Sub2ApiCustom.ps1 -RepoPath TARGET
```

Report missing markers by feature. Distinguish source presence from runtime enablement and database migration state.

## Safety and Data Boundaries

- The deployment script preflights all patches in a temporary Git worktree before touching the target.
- Never run it against a dirty target. Never use `git reset --hard`, forced checkout, or recursive deletion of the user's repository.
- Applying source does not modify production data. Before restarting a server that will run migration 224, remind the user to back up the production database according to their normal process.
- Do not claim a running service is updated until the user has explicitly requested and completed the deployment/restart step.
- Do not silently enable the feature in an unknown production config; identify the exact config file and obtain direction if changing it is outside the requested scope.
