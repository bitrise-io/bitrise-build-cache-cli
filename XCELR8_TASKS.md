# Xcelr8 — Task List

Hackathon project — macOS desktop companion app for Bitrise Build Cache.
Started 2026-05-06.

Backing kick-off doc: [Project Kick-off: Build Cache Desktop App](https://bitrise.atlassian.net/wiki/spaces/RD/pages/4990238757/Project+Kick-off+Build+Cache+Desktop+App)

## Architecture — three sides

1. **UI** — macOS app shell. Hackathon scope: shallow React Native wrapper.
   _Note:_ kick-off doc decided **Native Swift** for the productized version. RN is a hackathon-speed choice; flag if it diverges from final plan.
2. **CLI** — `bitrise-build-cache-cli` (this repo). Hosts the `internal/invocations` client and exposes data + health to the UI.
3. **BE** — `bitrise-io/bitrise-website` `BuildCache::InvocationsController`. Source of invocation data. See `reference_be_invocations_api.md`.

---

## UI — macOS shell (RN)

| #   | Task                                                                       | Notes                                                              |
| --- | -------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| U1  | Bootstrap RN-macOS project, menu bar lifecycle                             | Tray app skeleton                                                  |
| U2  | Sign-in screen — PAT entry; OAuth-via-browser stub                         | Confluence still TBD on OAuth                                      |
| U3  | Setup wizard — 5 parts: sign-in / tool select / activate / proxies / wait | Mirrors kick-off §3.2                                              |
| U4  | Menu bar 3-state indicator (green / yellow / red) + drop-down              | §3.1, five health rows                                             |
| U5  | Health drop-down rows wire to CLI `healthcheck --json`                     | auth, BC API, ccache proxy, xcelerate proxy, tool config           |
| U6  | Last-5 local invocations list (calls CLI `invocations list`)               | §3.1 + §3.4                                                        |
| U7  | Home — health banner + stat cards                                          | cached count / hit rate P50 / time saved 7d — depends on BE stats  |
| U8  | Proxies view — per-proxy status, restart, log tail                         | §3.5                                                               |
| U9  | Updates view — notify-only + one-click apply                               | §3.6 — CLI / configs / app                                         |
| U10 | Native notifications — proxy stopped, token expired                        | §3.7                                                               |
| U11 | App self-update                                                            | Sparkle-equivalent compatible with RN-macOS                        |
| U12 | CLI install / upgrade flow driven from app                                 | App owns CLI version on disk                                       |

## CLI — `bitrise-build-cache-cli`

| #   | Task                                                              | Existing?                                        | Gap                                                                       |
| --- | ----------------------------------------------------------------- | ------------------------------------------------ | ------------------------------------------------------------------------- |
| C1  | Top-level `healthcheck` command, JSON output                      | partial (ccache only)                            | Aggregate auth + BC-API + ccache + xcelerate + tool-config                |
| C2  | `auth check` — validate token against BE without running a build  | no                                               | New — call BC capabilities or `/me` with token                            |
| C3  | `cache ping` — Build Cache API reachability                       | internal only (`internal/build_cache/kv/methods.go`) | Surface as user-facing subcommand                                     |
| C4  | `xcelerate proxy health-check` / `status`                         | no (only start / stop)                           | Add — mirror ccache `health-check`                                        |
| C5  | ccache storage helper "no-timeout" mode for desktop app           | unclear                                          | Verify lifecycle; add idle-disable flag if missing                        |
| C6  | `invocations list / get / tasks` user-facing cobra commands       | client done in `internal/invocations`            | Add cobra cmds + flags + JSON output                                      |
| C7  | `invocations stats` — local aggregate via BE                      | no                                               | cached count, hit rate P50, time saved (uses BE list + aggregate)         |
| C8  | `proxy logs --tail` for ccache + xcelerate                        | partial — log paths known                        | Wrap + JSON-line streaming                                                |
| C9  | Benchmark run trigger (`benchmark run <project>`)                 | BE-driven phase only                             | Allow user-initiated benchmark from CLI                                   |
| C10 | Self-update — `update check` / `update apply`                     | partial (`internal/dependencies/cli.go` downloads) | Compare GitHub releases vs local version, apply                         |
| C11 | Notification hook — CLI emits events when a proxy crashes         | no                                               | File / socket signal; app watches and renders OS notification             |
| C12 | Stable JSON output mode for every command the app calls           | inconsistent                                     | Standard contract: `--output json`                                        |

### CLI capability snapshot (audit findings)

- **Auth:** `internal/config/common/auth.go` parses `BITRISE_BUILD_CACHE_AUTH_TOKEN` and the JWT in `BITRISEIO_BITRISE_SERVICES_ACCESS_TOKEN`. No standalone backend-validation command.
- **BC API reachability:** `internal/build_cache/kv/methods.go` has `GetCapabilities()` (5 s timeout) and `GetCapabilitiesWithRetry()` (10 retries). Buried; not surfaced as a user command.
- **ccache health:** `cmd/ccache/health_check_storage_helper.go` — IPC ping with configurable timeout / interval (defaults 10 s / 100 ms). `pkg/ccache/storage_helper.go` exposes `HealthCheck()`.
- **xcelerate proxy:** `cmd/xcode/xcelerate_start_proxy.go` + `xcelerate_stop_proxy.go`. PID-file-driven SIGTERM → SIGKILL on stop. **No status command.** Logs at `~/.local/state/xcelerate/logs/proxy-<id>-out.log` and `proxy-err.log`.
- **Invocations API client:** `internal/invocations/` — `List`, `Get`, `GetSummary`, `GetGradleTasks`, `GetBazelTargets`, `GetChildInvocations`, `GetSiblingInvocations`. Done.
- **Local stats:** none persisted. `pkg/common/childstats/childstats.go` is in-memory per build. No local DB.
- **Benchmark mode:** `internal/config/common/benchmark.go` is BE-driven phase only (`baseline` / `warmup`). No user-initiated benchmark.
- **Versioning:** `cmd/common/version.go` prints local version. `internal/dependencies/cli.go` can download a CLI release. No update-check command wires the two.
- **OAuth:** none.
- **Process spawn:** ccache helper detached via `cmd.Start()` in `internal/ccache/socket.go`. xcelerate proxy is started in foreground; caller backgrounds.

## BE — `bitrise-website` (`BuildCache::InvocationsController`)

| #  | Task                                                                                              | Status                                                                                               |
| -- | ------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| B1 | Filter invocations by user (`user_slug` / `user_email`)                                           | Missing — already noted in `reference_be_invocations_api.md`                                         |
| B2 | Filter invocations by hostname (this-Mac-only view, §3.4)                                         | Missing — `Invocation.hostname` is stored, just needs `safe_params.permit` + plumb to sink client    |
| B3 | Filter by `provider_id` empty / `local` (CI vs local split)                                       | Verify — may already be possible via `ci_provider`                                                   |
| B4 | Aggregate stats endpoint: count + hit-rate P50 + time-saved per workspace + window + filters     | Missing — Home view (U7) depends on this, otherwise UI computes client-side from list pages         |
| B5 | OAuth sign-in / browser flow for desktop app (if going beyond PAT)                                | TBD per kick-off — confirm whether existing Bitrise OAuth can issue a PAT-equivalent for the app    |
| B6 | Confirm `Get Capabilities` endpoint as canonical reachability + auth-validity probe              | Likely already returns 200 with auth; verify behavior with bad / expired token                       |
| B7 | Stable presenter response contract for invocations index / show                                   | Document `BuildToolInvocationInfoPresenter#to_h` shape so CLI Go structs don't drift                 |

---

## Cross-cutting questions still open

- **OAuth vs PAT** — kick-off §6 row 5 still TBD. PAT is the immediate path; OAuth is the polished v1 story.
- **Healthy-setup criteria owner** — kick-off §7 still needs an owner.
- **v0 / v1 target dates** — TBD.
- **Local-only filter mechanism** — hostname (B2) vs. provider_id (B3) vs. a new `is_local` field. Decide before B-side work starts.

## Source-of-truth references

- Kick-off: <https://bitrise.atlassian.net/wiki/spaces/RD/pages/4990238757/Project+Kick-off+Build+Cache+Desktop+App>
- BE controller: `bitrise-io/bitrise-website` → `components/bff/app/controllers/build_cache/invocations_controller.rb`
- BE routes: `bitrise-io/bitrise-website` → `config/routes.rb` namespace `:build_cache`
- CLI invocations client: `internal/invocations/` (this repo)
- Memory: `project_xcelr8_hackathon.md`, `reference_be_invocations_api.md`
