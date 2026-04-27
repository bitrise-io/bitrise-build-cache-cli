---
name: release
description: Drive the full release process end-to-end across gradle-plugins, CLI, step, and steplib repos. Use when the user asks to release, publish, or deploy a new version.
user_invocable: true
---

# Release Process

Releasing a new version is a multi-step process across several repositories.

**IMPORTANT: Drive the ENTIRE process end-to-end in a single conversation.** Use the Bitrise MCP server to monitor build statuses (poll every 30s), check for triggered workflows in downstream repos, and move to the next step as soon as the previous one completes. Do not stop and wait for the user between steps.

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

- **Do NOT mark it as "latest"** — another CI job handles that
- Follow the format of existing releases for release notes
- **Version numbering — always ask the user** which semver bump to apply (patch, minor, or major). Use these guidelines as defaults:
  - **Patch** bump: dependency-only updates (e.g., plugin version bumps) or bug fixes
  - **Minor** bump: new features or non-breaking behavioral changes in the CLI
  - **Major** bump: breaking changes
- If this is a gradle-plugin-only update (no CLI code changes), the CLI version should be a **patch** bump because only a dependency was updated
- Check the latest existing release tag to determine the next version

### 7. Wait for step auto-update PRs

The CLI release triggers auto-update PRs in **four** step repos (all use unified CI app `48fa8fbee698622c`). The PR title will be "feat: Release new CLI". Monitor CI on all four, then approve and merge:

1. **Gradle step:** `bitrise-steplib/bitrise-step-activate-gradle-remote-cache`
2. **Xcode step:** `bitrise-steplib/bitrise-step-activate-build-cache-for-xcode`
3. **Gradle features step:** `bitrise-steplib/bitrise-step-activate-gradle-features` (experimental, no release needed)
4. **React Native features step:** `bitrise-steplib/bitrise-step-activate-react-native-features` (experimental, no release needed)

```bash
# For each step repo:
gh pr review --approve --repo <REPO> <PR_NUMBER>
gh pr merge --squash --auto --repo <REPO> <PR_NUMBER>
```

Always wait for CI to pass. Use `--squash` (merge commits are not allowed on these repos).

### 8. Create step GitHub releases

Create GitHub releases for the **Gradle step** and the **Xcode step** (NOT the gradle-features step — it doesn't need releases).

- These **can** be marked as "latest"
- Follow the format of existing releases for release notes — only include "## What's Changed" with bullet points (changelog is added automatically)
- **Version numbering: the step version bump should match the CLI version bump.** Patch CLI bump -> patch step bump. Minor CLI bump -> minor step bump
- Check the latest existing release tag in each repo to determine the next version

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
