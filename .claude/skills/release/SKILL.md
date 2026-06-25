---
name: release
description: Drive the full release process end-to-end across gradle-plugins, CLI, step, and steplib repos. Use when the user asks to release, publish, or deploy a new version.
user_invocable: true
---

# Release Process

Releasing a new version is a multi-step process across several repositories.

**IMPORTANT: Drive the ENTIRE process end-to-end in a single conversation.** Use the Bitrise MCP server to monitor build statuses (poll every 30s), check for triggered workflows in downstream repos, and move to the next step as soon as the previous one completes. Do not stop and wait for the user between steps.

## ✅ Definition of done — a release ships through MULTIPLE channels

**A release is NOT done until every channel below is MERGED and VERIFIED — not merely opened.** Reporting "released" / "the steps are out" off a PR that *exists* but hasn't merged is the exact failure this gate exists to prevent. A just-shipped feature that isn't appearing where you expect is most often a release that didn't actually finish a channel — suspect that before rollout-timing or usage explanations.

When you report status, report it **per channel**, and state what you actually confirmed (merged/verified) vs. what is still open. "The bot will handle it" is not done — go look. The channels:

1. **CLI GitHub release** — promoted out of prerelease, all expected assets present (Step 6).
2. **`verify-release`** — green (Step 6).
3. **`bump-prebooting` PR** (preboot VM image) — **merged** (Step 6b). This is how provision-injected features, e.g. the gradle-mirrors init script, reach the *default* fleet. The bypass-merge can stall and need a manual approval.
4. **Step auto-update PRs in all FIVE consumer repos** — **merged** (Step 7).
5. **Step GitHub releases** — cut for the scoped steps (Step 8).
6. **Steplib PRs** — merged (Step 9).

Two distinct delivery paths exist and a complete release must finish BOTH: the **default fleet** gets CLI-driven features via **provisioning/preboot** (channel 3); customers who **pin a CLI version** get them via the **steps** (channels 4–6). Confirming one says nothing about the other.

## ⚠ Critical path — read before doing anything

The `install/installer.sh` script and the binaries attached to every CLI GitHub release are **on the critical path of every Bitrise build — not just builds that opt into the build cache**. The Bitrise default workflow runs the gradle-mirrors activation step (and other CLI-driven steps) unconditionally, and each of those pipes `installer.sh` to `sh` and fetches the platform tarball + checksum from the latest non-prerelease GitHub release. If any of these break — installer script, binaries, checksum file, the wrong release marked as latest — the CLI install fails, the mirror activation soft-fails, and Maven Central requests bypass the Bitrise proxy on the entire fleet.

That failure mode caused the [2026-04-28 Maven Central rate-limit incident](https://bitrise.atlassian.net/wiki/spaces/INCIDENT/pages/4980998155/2026-04-28+-+Postmortem+for+incident-2026-04-28-mavencentral-too-many-requests-5238).

Concrete rules:

- **Always create the CLI GitHub release as `--prerelease`** (see step 6). The installer ignores prereleases when resolving `latest`, so an empty / half-uploaded release cannot poison builds.
- **Never let an empty release be marked `latest`.** If goreleaser fails midway, leave the release as prerelease until you have manually verified the assets list is complete.
- **Treat any failure of the CLI `release` workflow as a drop-everything-and-fix incident.** Don't move on to step releases until the CLI release workflow is green AND the v2.6.x release has all 8 expected assets (6 platform tarballs, checksums.txt, both verification XMLs).
- **Smoke-test installer.sh edits on a real Bitrise build before merging.** The release flow does not regenerate this file. PR CI does exercise it (see `pr-release-check-{linux,mac}` workflows) but a real Bitrise build is still the canonical smoke test.
- **The release workflow mirrors `install/installer.sh` to GAR** twice: once as `installer.sh:<tag>:installer.sh` (immutable, audit trail) and once as `installer.sh:latest-pointer:installer.sh` + `installer.sh:latest-pointer:VERSION` (mutable pointer + bare semver, refreshed via delete-then-upload each release). GAR rejects the literal `latest` as a reserved version_id, so the mutable view uses `latest-pointer`. The `latest-pointer` view is the documented carve-out from the `#327` immutability rule — safe because it's only consulted when the primary GitHub path is already failing. After every release, the **`verify-release` workflow** runs (chained after `release` via the `release-and-verify` pipeline) and executes `scripts/verify_release.sh` to assert the GH happy path and the GAR-only fallback both work end-to-end. Verification failures post to Slack and can be retried independently of `release` without re-cutting the tag.

### Brew tap is best-effort, NOT critical path

`bitrise-io/homebrew-bitrise-build-cache` is a nice-to-have publication target, not part of the install flow used by Bitrise builds. The "Publish Homebrew formula" step in `bitrise.yml`'s `release` workflow runs goreleaser with only the brew publisher and is marked `is_skippable: true`, so a tap-permission failure (e.g. 404 from `PUT /Formula/bitrise-build-cache.rb`) won't break the release.

If the brew step fails:

- Confirm the release workflow's overall status is still green.
- Verify the GitHub release has all 8 expected assets, the GAR mirror uploads (binaries + checksums + `installer.sh:<tag>` + `installer.sh:latest-pointer:{installer.sh,VERSION}`) succeeded, and the chained `verify-release` workflow (running `scripts/verify_release.sh`) passed — all critical. A `verify-release` failure does NOT mean re-cut the tag — fix the underlying issue and re-run the `verify-release` workflow alone via the Bitrise UI.
- Fix the tap separately. The most common cause is the bot user behind `GIT_BOT_USER_ACCESS_TOKEN` not being a collaborator with push access on `bitrise-io/homebrew-bitrise-build-cache`. Add them in the tap repo's settings.
- Do NOT block other step releases on this — re-running the brew publish is a separate concern.

## Two Entry Points

A release can be triggered by:

1. **Gradle plugin update:** A change merged in `bitrise-io/gradle-plugins` triggers the publish pipeline, which auto-triggers the CLI's `update_plugins` workflow. **Start from Step 1.**
2. **CLI-only changes:** Direct code changes to the CLI itself (e.g., Xcode fixes, new features) merged via a normal PR. The gradle-plugins steps don't apply. **Start from Step 5** (the CLI PR is already merged).

## Key Resources

| App | Bitrise App ID | GitHub Repo |
|-----|---------------|-------------|
| gradle-plugins | `cdb16849-294e-48c4-8623-18ade050bd0e` | `bitrise-io/gradle-plugins` |
| bitrise-build-cache-cli | `1a2ddc0a-bab0-4db1-9b78-4c13aae180ba` | `bitrise-io/bitrise-build-cache-cli` |
| Gradle step (unified CI) | `48fa8fbee698622c` | `bitrise-steplib/bitrise-step-activate-gradle-remote-cache` |
| Xcode step (unified CI) | `48fa8fbee698622c` | `bitrise-steplib/bitrise-step-activate-build-cache-for-xcode` |
| Gradle features step (unified CI) | `48fa8fbee698622c` | `bitrise-steplib/bitrise-step-activate-gradle-features` |
| React Native features step (unified CI) | `48fa8fbee698622c` | `bitrise-steplib/bitrise-step-activate-react-native-features` |
| Gradle mirrors step (pinned-version consumer) | _verify_ | `bitrise-steplib/bitrise-step-activate-gradle-mirrors` |
| Steplib | — | `bitrise-io/bitrise-steplib` |

## Steps

### 1. Merge PR in gradle-plugins (skip if CLI-only change)

Merging a PR to `main` in `bitrise-io/gradle-plugins` triggers the `publish-all` pipeline on the gradle-plugins Bitrise app. Only release modules that have version bumps in their `gradle.properties`.

### 2. Publish workflows auto-skip unchanged modules (skip if CLI-only change)

Each publish workflow automatically checks if its module's `VERSION_NAME` was bumped in the current commit. If not, it skips publishing and exits successfully. No need to manually abort workflows.

### 3. Monitor publish builds (skip if CLI-only change)

Poll the publish build(s) every ~30 seconds until they complete. Report status to the user.

### 4. Kick off CLI update workflow (skip if CLI-only change)

The publish pipeline should automatically trigger an `update_plugins` workflow in the CLI app `1a2ddc0a-bab0-4db1-9b78-4c13aae180ba`. Check for running builds in that app. If the workflow wasn't triggered, manually trigger `update_plugins`.

### 5. Monitor CLI update and merge PR (skip if CLI-only change — PR is already merged)

The `update_plugins` workflow creates a PR in `bitrise-io/bitrise-build-cache-cli`. Monitor the CI pipeline. If there are flaky cache hit rate failures, rebuild them (see "Flaky E2E tests" below). Once all checks pass:

```bash
gh pr review --approve --repo bitrise-io/bitrise-build-cache-cli <PR_NUMBER>
gh pr merge --merge --auto --repo bitrise-io/bitrise-build-cache-cli <PR_NUMBER>
```

**NEVER use `--admin` to bypass checks — always wait for CI to go green before merging.**

### 6. Create CLI GitHub release

Create a GitHub release in `bitrise-build-cache-cli`.

- **MUST mark it as `--prerelease`.** The release workflow uploads the binaries asynchronously; until those land, the release is empty. Marking it as latest (or as a regular release) at create-time makes a binary-less release "current," which breaks any consumer that downloads the latest asset. A separate CI job promotes the release out of prerelease once the binaries are appended.
- **Do NOT pass `--latest`.** Without `--latest`, GitHub will not auto-promote a prerelease; with `--latest` it would, defeating the prerelease gate.
- Example: `gh release create vX.Y.Z --repo bitrise-io/bitrise-build-cache-cli --title vX.Y.Z --prerelease --notes "..."`
- Follow the format of existing releases for release notes
- **Version numbering — always ask the user** which semver bump to apply (patch, minor, or major). Use these guidelines as defaults:
  - **Patch** bump: dependency-only updates (e.g., plugin version bumps) or bug fixes
  - **Minor** bump: new features or non-breaking behavioral changes in the CLI
  - **Major** bump: breaking changes
- If this is a gradle-plugin-only update (no CLI code changes), the CLI version should be a **patch** bump because only a dependency was updated
- Check the latest existing release tag to determine the next version

### 6b. Verify the preboot bump (`bump-prebooting`)

The `release-and-verify` pipeline chains a `bump-prebooting` workflow after `verify-release`. It opens an auto-merging PR in `bitrise-io/build-prebooting-deployments` bumping `BITRISE_BUILD_CACHE_CLI_VERSION` + the per-arch sha256 in the two startup-script extensions, and tries to bypass-merge it as `bitrise-infrabot`. This is the channel that delivers provision-injected features (e.g. the gradle-mirrors init script) to the **default fleet** — a release that skips it ships to nobody on the default path.

- **Confirm the PR actually MERGED — don't assume the bot finished.** The bypass-merge can fail (`base branch policy prohibits the merge`) even when the CLI release is fully green.
- `gh pr merge --admin` does **not** work there (admins can't bypass that repo's rule). Fix: approve the bump PR by hand — it's authored by the bot, so a maintainer approval satisfies the 1-approval rule — then `gh pr merge --squash --delete-branch`. Verify the diff is the version + the two sha256s from this release's `checksums.txt`. Do NOT re-cut the tag.

### 7. Wait for step auto-update PRs

The CLI release triggers auto-update PRs in **five** consumer repos. Monitor CI, then approve and merge each. The first four use unified CI app `48fa8fbee698622c` and the PR title "feat: Release new CLI":

1. **Gradle step:** `bitrise-steplib/bitrise-step-activate-gradle-remote-cache` — released for every CLI version.
2. **Xcode step:** `bitrise-steplib/bitrise-step-activate-build-cache-for-xcode` — released for every CLI version.
3. **React Native features step:** `bitrise-steplib/bitrise-step-activate-react-native-features` — released, but releases are **not 1:1 with CLI releases** (each step release usually catches up across several intervening CLI patch versions; release when CLI changes matter for RN, e.g. an Xcode or Gradle-side improvement that RN builds benefit from).
4. **Gradle features step:** `bitrise-steplib/bitrise-step-activate-gradle-features` — truly experimental, no GitHub release flow yet (only a single early steplib PR exists). Merge the auto-update PR but do not cut a GitHub release until that changes.
5. **Gradle mirrors step:** `bitrise-steplib/bitrise-step-activate-gradle-mirrors` — used by customers who **pin a specific CLI version** (the default fleet gets the mirror init script via provisioning/preboot instead, so this step is the delivery channel only for pinned-version builds). ⚠ Do **not** assume its auto-update fires — it has lagged the CLI badly (stuck at v2.6.1 / release 0.2.1 while the CLI was at v2.8.x). If no auto-update PR appears, **open the bump PR manually**. Its bump PR title is `chore: bump bitrise-build-cache-cli to vX.Y.Z` (not "feat: Release new CLI"), and it uses 0.x step versioning.

```bash
# For each step repo:
gh pr review --approve --repo <REPO> <PR_NUMBER>
gh pr merge --squash --auto --repo <REPO> <PR_NUMBER>
```

Always wait for CI to pass. Use `--squash` (merge commits are not allowed on these repos).

### 8. Create step GitHub releases

Create GitHub releases for whichever of the five step repos the user actually wants to release (default: **Gradle step** + **Xcode step**; **React Native features step** when the CLI change is RN-relevant; **Gradle mirrors step** when its CLI bump matters for pinned-version users or it has fallen behind). The **Gradle features step** does not have a GitHub release flow yet — skip it. The **Gradle mirrors step** has its own 0.x release flow — cut a 0.x release after its bump PR merges.

- These **can** be marked as "latest"
- Follow the format of existing releases for release notes — only include "## What's Changed" with bullet points (changelog is added automatically)
- **Version numbering: the step version bump should match the CLI version bump.** Patch CLI bump → patch step bump. Minor CLI bump → minor step bump.
- For step repos that don't release for every CLI version (currently the RN features step), the changelog should note the headline change for this release **and** mention any intervening CLI versions that are being picked up at the same time — readers should be able to tell what they're getting.
- Check the latest existing release tag in each repo to determine the next version
- The user may explicitly scope the release to a subset of step repos ("only release xcode and rn-features"). Honor that — do not release the others. Merging their auto-update PRs is still fine and expected (keeps the dependency current); skipping is only about the GitHub release / steplib PR.

### 9. Merge steplib PRs

After the step releases, PRs appear in `bitrise-io/bitrise-steplib` for each released step. They may need a rebase.

```bash
gh pr review --approve --repo bitrise-io/bitrise-steplib <PR_NUMBER>
gh pr merge --squash --auto --repo bitrise-io/bitrise-steplib <PR_NUMBER>
```

Always wait for CI to pass — never bypass branch protection. The steplib repo requires squash merges (merge commits are blocked).

## Flaky E2E tests — cache hit rate

The CLI repo's `features-e2e` pipeline includes cache hit rate assertions. If a test (e.g., `feature-e2e-gradle-7`) fails with `cacheHitRate: want != 0, got 0`, it's likely because the cache items were evicted since the last run, or because of co-located caches across multiple data centers (builds may land on a different DC than the one that has the warm cache). **Keep rebuilding the failed workflows** — it may take 2-3 attempts to get a green build. This is not a real failure.

## Auto-update scripts

- Gradle plugins repo: `/scripts/`
- CLI repo: `https://github.com/bitrise-io/bitrise-build-cache-cli/tree/main/scripts`
- Step CI: Uses the unified Bitrise CI app `48fa8fbee698622c` — just wait for the auto-update PR and steplib PR.
