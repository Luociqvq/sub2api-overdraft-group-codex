# Sub2API Custom Deployer

A local Codex plugin that reapplies and verifies three Sub2API customizations
after upstream updates. The patch set is pinned to Sub2API `v0.1.177`.

## What the three features do

1. **Codex 5h/7d quota overdraft** — when an OAuth account reports a quota
   boundary, the server performs bounded probes and keeps scheduling only when
   a valid upstream response proves that the account can still serve requests.
   It records the probe state and overdraft usage; this is not an unlimited
   quota bypass.
2. **Armor group** — adds group-level Codex instructions and the associated
   admin/UI fields.
3. **Bounded 429 reconnect** — for selected upstream `429` responses, resets
   the affected connection pool and retries with a fresh connection within a
   fixed bound. Other authentication, model, proxy, and transport failures
   continue to use the normal handling.

## What this repository changes (and does not change)

This repository contains versioned patches, marker checks, and PowerShell
helpers for the **Sub2API server source tree**. It does not contain account
credentials, OAuth tokens, database exports, or Cockpit runtime state.

The patches are server-side Go/frontend changes. They do **not** patch,
replace, or inject into `cockpit-tools.exe` or `cockpit-cliproxy.exe`, and the
plugin does not add a provider automatically. Cockpit is connected separately
to the running OpenAI-compatible Sub2API endpoint (see [Cockpit Tools](#cockpit-tools)).

The deployment script preflights all patches in a temporary Git worktree,
requires a clean target worktree, creates a review branch, and never commits,
pushes, restarts a service, or changes production data on your behalf.

## Quick start

### A. Apply the patches to a Sub2API checkout

Install this directory as a Codex plugin, then ask Codex:

```text
Use sub2api-custom-deployer to apply the three custom features to TARGET and run the focused tests.
```

Or run the helpers directly from this repository:

```powershell
# Preflight only; makes no changes to TARGET.
pwsh -File scripts/Deploy-Sub2ApiCustom.ps1 -RepoPath TARGET -CheckOnly

# Create a review branch, apply patches, leave a normal unstaged diff, and run backend checks.
pwsh -File scripts/Deploy-Sub2ApiCustom.ps1 -RepoPath TARGET -RunTests

# Check integration markers without applying anything.
pwsh -File scripts/Test-Sub2ApiCustom.ps1 -RepoPath TARGET
```

`TARGET` must be a Git checkout of Sub2API with a clean worktree. Review the
result with `git diff`, commit it yourself, build/deploy Sub2API, and restart
that service. The helper intentionally stops before those operational steps.
For a newer upstream version, use the feature anchors in
`skills/sub2api-custom-deployer/SKILL.md` and resolve conflicts by behavior;
do not copy whole old files over newer upstream code.

### B. Enable the overdraft feature in the running server

In the **actual** Sub2API runtime configuration (often `deploy/config.yaml`),
set:

```yaml
gateway:
  codex_quota_overdraft_enabled: true
```

Restart Sub2API after changing the configuration. Set the flag to `false` and
restart to return to the upstream scheduler behavior.

## Cockpit Tools

Cockpit Tools is a separate Windows client/sidecar. Its local
`cockpit-cliproxy.exe` maintains its own `quota-pool-state.json` and
`quota-reserve.json` files and exposes a local quota endpoint. Those files are
not part of this repository and should remain outside source control.

To route Cockpit through a running local Sub2API overdraft service:

1. Start the patched Sub2API server and confirm the runtime flag above is on.
2. In Cockpit's provider/API-service UI, add an OpenAI-compatible provider.
3. Enter the server URL and key (replace placeholders; do not commit secrets):

   ```text
   Base URL: http://HOST:PORT/v1
   API key: TOKEN
   ```

4. Send a small test request from Cockpit. If the provider cannot connect,
   check that `HOST:PORT` is reachable from the Cockpit process and that the
   key is accepted by Sub2API.

The local Cockpit quota endpoint reports Cockpit's own account-pool state; it
does not prove that Sub2API's server-side overdraft flag is enabled. Verify
the two systems independently.

## Verification

Static marker verification:

```powershell
pwsh -File scripts/Test-Sub2ApiCustom.ps1 -RepoPath TARGET
```

Focused backend checks (requires the target's Go toolchain and dependencies):

```powershell
pwsh -File scripts/Test-Sub2ApiCustom.ps1 -RepoPath TARGET -RunTests
```

Add `-Frontend` when running the focused frontend checks as well. See
`assets/docs/overdraft-customization.md` for the complete behavior,
configuration, upgrade procedure, and troubleshooting notes.

## License

LGPL-3.0-or-later. See [LICENSE](LICENSE) and [NOTICE.md](NOTICE.md).
