# ACI-5540 — Infinum AmerigoDemo: smaller dSYM + dsymutil "No such file or directory" warnings when Xcode Build Cache enabled

**Date gathered:** 2026-10-01 / 2026-10-02 (UTC)
**Reporter (per ticket):** Infinum (customer); internally surfaced via Slack #? by Viktor Benei.
**Customer project:** AmerigoDemo (iOS, part of `infinum/ios-amerigo-evnavi-sdk` repo).
**Bitrise app:** `2382dd7a-57b8-4935-9d47-73df6c50b7be` — `ios-amerigo-evnavi-sdk` (Infinum org `762310816667af0a`).

Scope: factual data collection only. Conclusions go in the "Preliminary findings" section at the bottom.

---

## Builds under investigation

| Field | Cache ON (build 1671) | Cache OFF (build 1704) |
|---|---|---|
| Build slug | `b83372cf-c88a-46c5-845c-54e064a6aa15` | `0c9c6f65-1aa0-41c6-b6fb-43dbcbd0281b` |
| Short SHA (ticket) | b83372cf | 0c9c6f65 |
| Build number | 1671 | 1704 |
| Triggered at | 2026-09-21T12:43:18Z | 2026-09-24T13:07:57Z |
| Status | success | success |
| Workflow | `deploy-demo-app` | `deploy-demo-app` |
| Stack | `osx-xcode-26.6.x` | `osx-xcode-26.6.x` |
| Machine type | `g2.mac.medium` | `g2.mac.medium` |
| Commit | `8806ae7bd7ac39e46cc6dda315c6095b395172d0` | `207b8ef6f82db58b38e9e602c95e3b69e15e3758` |
| Tag | `ci/ev-navigation-demo/2026-09-21T14-41-56` | `ci/ev-navigation-demo/2026-09-24T15-06-51` |
| Full log line count | 3186 | 2516 |

**Note on commit:** The ticket says "same commit, different builds". Bitrise metadata disagrees — the two builds have different commit hashes (both on the same `deploy-demo-app` tag-pattern trigger). Diff-only facts collected below treat commit as a variable we did not control for.

---

## Xcode / stack facts (identical on both builds)

- `Xcode 26.6 (17F113)` (log line 681 build 1671; log line 694 build 1704)
- Project path: `Demo/AmerigoDemo.xcodeproj`
- Scheme: `AmerigoDemo`
- Configuration: `Release` (visible in `xcodebuild -showBuildSettings` invocations, log line 886 / 894)
- Target: `AmerigoDemo`
- `DEBUG_INFORMATION_FORMAT=dwarf-with-dsym` (confirmed in the xcelerate archive log, line 31336 — exported into the "Upload dsyms to Crashlytics" script phase env)
- `xcbeautify` log formatter version: 3.1.4
- xcode-archive step pin: `xcode-archive@6.1` in the `deploy-demo-app` workflow
- Fixed xcodebuild options: `-disableAutomaticPackageResolution`, `compile_bitcode: no`, `upload_bitcode: no`, `distribution_method: app-store`

## bitrise.yml

Byte-identical between builds 1671 and 1704 (`cmp` → zero exit; 15,649 bytes each). All step pins, env, and run_if blocks match. The only difference between the two runs is runtime state (see activate section below).

Activate step is placed in the `bundle::Prepare-app-deploy` step bundle, pin `activate-build-cache-for-xcode@0`, with no inputs (defaults). This bundle is referenced by `deploy-demo-app` as its first step.

---

## Activate step runtime behavior

### Build 1671 (cache ON) — activate succeeded

Log excerpt (lines 324–363):

```
| id: activate-build-cache-for-xcode
bitrise-io/bitrise-build-cache-cli info checking GitHub for tag 'v3.10.0'
bitrise-io/bitrise-build-cache-cli info found version: 3.10.0 for v3.10.0/darwin/arm64
bitrise-io/bitrise-build-cache-cli info installed /tmp/bin/bitrise-build-cache
Exported BITRISE_BUILD_CACHE_BENCHMARK_PHASE_XCODE to the current process environment
Using new proxy socket path: /var/folders/.../T/xcelerate-proxy.sock
[12:43:34] Config saved to: /Users/vagrant/.bitrise-xcelerate/config.json
[12:43:34] Copied CLI to /Users/vagrant/.bitrise-xcelerate/bin/bitrise-build-cache-cli
Wrote xcodebuild wrapper script: /Users/vagrant/.bitrise-xcelerate/bin/xcodebuild
Wrote xcrun wrapper script: /Users/vagrant/.bitrise-xcelerate/bin/xcrun
```

- **CLI version installed:** `3.10.0`
- Xcelerate xcodebuild/xcrun wrappers were written and PATH-prepended.
- xcelerate proxy started at archive time (log line 904): `Cache enabled, starting xcelerate proxy connecting to: grpcs://bitrise-accelerate.services.bitrise.io`, PID 8852.

### Build 1704 (cache OFF) — activate "deactivated by workspace entitlement"

Log excerpt (lines 335–390):

```
| id: activate-build-cache-for-xcode
| version: 0.28.3
Checking whether Bitrise Build Cache is activated for this workspace ...

Bitrise Build Cache is not activated in this build.

You have added the **Activate Bitrise Build Cache for Xcode** add-on step to your workflow.

However, you don't have an activate Bitrise Build Cache Trial or Subscription for the current workspace yet.
...
+ exit 2
exit status 2
This Step failed, but it was marked as "is_skippable", so the build continued.
+---+---------------------------------------------------------------+----------+
| ! | Build Cache for Xcode (Failed)                                | 1.58 sec |
```

- Build Cache entitlement was **off at workspace level** when 1704 ran. The step short-circuited before installing the CLI or wrappers.
- No xcelerate proxy, no compile cache flags, no wrapped xcodebuild on 1704.

---

## xcodebuild invocation differences

### Build 1671 (via xcelerate wrapper) — one line (abridged):

```
/usr/bin/xcodebuild archive \
  -project /Users/vagrant/git/Demo/AmerigoDemo.xcodeproj \
  -scheme AmerigoDemo \
  -xcconfig /var/folders/.../T/1770636053/temp.xcconfig \
  -archivePath /var/folders/.../T/xcodeArchive1883410870/AmerigoDemo.xcarchive \
  -destination generic/platform=iOS \
  -disableAutomaticPackageResolution \
  COMPILATION_CACHE_ENABLE_DETACHED_KEY_QUERIES=YES \
  SWIFT_ENABLE_COMPILE_CACHE=YES \
  SWIFT_ENABLE_EXPLICIT_MODULES=YES \
  SWIFT_USE_INTEGRATED_DRIVER=YES \
  COMPILATION_CACHE_ENABLE_DIAGNOSTIC_REMARKS=NO \
  CLANG_ENABLE_PREFIX_MAPPING=YES \
  COMPILATION_CACHE_REMOTE_SERVICE_PATH=/var/folders/.../T/xcelerate-proxy.sock \
  COMPILATION_CACHE_ENABLE_PLUGIN=YES \
  COMPILATION_CACHE_ENABLE_INTEGRATED_QUERIES=YES \
  CLANG_ENABLE_COMPILE_CACHE=YES \
  COMPILATION_CACHE_REMOTE_SUPPORTED_LANGUAGES="swift c c++ objective-c objective-c++" \
  PROJECT_TEMP_DIR=/Users/vagrant/.bitrise/cache/xcode-ptd/a79b398ef85496e1 \
  CLANG_ENABLE_MODULES=YES \
  COMPILATION_CACHE_ENABLE_CACHING=YES \
  OTHER_CFLAGS="$(inherited) \
    -fdepscan-prefix-map=/Users/vagrant/.bitrise/cache/xcode-ptd/a79b398ef85496e1=/^obj \
    -fdepscan-prefix-map=/Users/vagrant/.bitrise/cache/xcode-dd/a79b398ef85496e1=/^dd \
    -fdepscan-prefix-map=/Users/vagrant/git/Demo=/^src \
    -fdepscan-prefix-map=/Users/vagrant=/^home" \
  -derivedDataPath /Users/vagrant/.bitrise/cache/xcode-dd/a79b398ef85496e1 \
  -resultBundlePath /var/folders/.../T/bitrise-xcelerate-e9bed4af-aa16-4e9d-b711-3f4e56de1cf5.xcresult
```

### Build 1704 — stock `xcodebuild`

Just `xcodebuild archive -project ... -scheme AmerigoDemo -configuration Release -destination generic/platform=iOS ...` with no COMPILATION_CACHE_* / SWIFT_ENABLE_COMPILE_CACHE / CLANG_ENABLE_COMPILE_CACHE / prefix-map flags. No xcelerate proxy involved.

---

## dsymutil failures (cache ON only)

### Where in the build

In both builds the only `GenerateDSYMFile` task is for the top-level `AmerigoDemo.app`:

```
GenerateDSYMFile .../BuildProductsPath/Release-iphoneos/AmerigoDemo.app.dSYM \
                 .../InstallationBuildProductsLocation/Applications/AmerigoDemo.app/AmerigoDemo
    cd /Users/vagrant/git/Demo
    /Applications/Xcode-26.6.0.app/Contents/Developer/Toolchains/XcodeDefault.xctoolchain/usr/bin/dsymutil \
      .../Applications/AmerigoDemo.app/AmerigoDemo \
      -o .../BuildProductsPath/Release-iphoneos/AmerigoDemo.app.dSYM
```

Build 1671: xcodebuild-archive artifact, line 30533–30536. Build 1704: same tool at the same point, with no warnings afterwards.

The warnings fire **immediately after the dsymutil invocation and before the next build step** (`[AmerigoDemo] Running script Upload dsyms to Crashlytics`). The warnings themselves are the raw dsymutil stdout, carried through xcbeautify (log lines 2011–2673 on build 1671).

### Volume

- `: No such file or directory` lines: **332** (build 1671), **0** (build 1704) [grep -c]
- `note: while processing` lines: **332** (build 1671), **0** (build 1704)
- Unique CAS IDs across both warning lines: **332** (each ID appears exactly twice — once in the warning, once in the paired `note: while processing`)
- Occurrences of `0~<base64>==` pattern: 664 total, 332 unique → ticket's "340" matches the count.

### Warning format (first 5 lines, after ANSI strip)

```
[AmerigoDemo] Generating AmerigoDemo.app.dSYM
warning: 0~6Y0DzOv9Uwgswnsl82XQkaHOq1SDraND4B6yQzpCYOqsWPiwqoV_W6brvaDd3GMDaS00hRkAOh_aE1ThzlgglA==: No such file or directory
note: while processing 0~6Y0DzOv9Uwgswnsl82XQkaHOq1SDraND4B6yQzpCYOqsWPiwqoV_W6brvaDd3GMDaS00hRkAOh_aE1ThzlgglA==
warning: 0~0ImhJpsn8h_9ImS9aUSTvkjMKTHiMaUOmt4qIEk2Hs41VUaSS6cmrEShNL6G1Q8pR-AkizbjhRA0LJp-8rEYLA==: No such file or directory
note: while processing 0~0ImhJpsn8h_9ImS9aUSTvkjMKTHiMaUOmt4qIEk2Hs41VUaSS6cmrEShNL6G1Q8pR-AkizbjhRA0LJp-8rEYLA==
warning: 0~rGrChPiJwEciDGAqq0YUpU9h4cQnn_imtuvxnxTe1Ss_YTWbv2c65vlxKDCK2ChCRkwe4vc9oF7No4mQU0-Srg==: No such file or directory
```

### Noteworthy warning variant (near end of cluster)

One warning (line 2669) prefixes the CAS ID with the project path:

```
warning: /Users/[REDACTED]/git/Demo/AmerigoDemo.xcodeproj/0~ojZxUvY4KDCc7Wi5mBY_rihACosJeJ1YBvM_rv51zHR8VW8Ce381hGC_tTgeDHxA-mL-5FGc-F_Hg4e1kEI88Q==: No such file or directory
note: while processing /Users/[REDACTED]/git/Demo/AmerigoDemo.xcodeproj/0~ojZx…==
```

This suggests dsymutil is appending the CAS id to a search path (the cwd at dsymutil time is `/Users/vagrant/git/Demo`), i.e. it tried `fopen("<cwd>/<cas-id>")` as a fallback once the CAS lookup returned nothing.

### Not present in logs

- No `swiftc` or `clang` lines emit `0~...: No such file or directory`.
- No `linker: ld` errors.
- No post-dsymutil error / exit non-zero — Xcode treats these as warnings only; `** ARCHIVE SUCCEEDED **` still fires.

### Compile cache stats for the archive invocation

```
CompilationCacheMetrics
note: 989 hits / 993 cacheable tasks (100%)

[Bitrise Analytics] Proxy blob stats: hits: 4427 (639 MB) / total: 4431 (99.91%). Uploaded blobs: 744 (48 MB)
[Bitrise Analytics] Proxy KV stats:  hits: 2018 / total: 2022 (99.80%). Uploaded KV blobs: 71 kB
[Bitrise Analytics] Xcode task stats: hits: 989 / total: 993 (99.60%)
```

From the proxy's own view there were only **4 blob misses** across the entire archive — yet dsymutil then printed **332 unique CAS-id "not found" warnings**. The 332 lookups dsymutil issued are therefore not the same transfer surface the xcelerate proxy counts (which covers compiler GETs/PUTs during the `xcodebuild archive` run, not post-link dsymutil queries).

---

## dSYM artifact comparison

### Zip sizes (artifact listing)

| Artifact | Cache ON (1671) | Cache OFF (1704) | Delta |
|---|---|---|---|
| `AmerigoDemo.dSYM.zip` | 31,220,073 B | 33,147,034 B | **−1,926,961 B** (−5.81%) |
| `AmerigoDemo.ipa` | 35,599,687 B | 35,587,924 B | +11,763 B (+0.03%) |
| `AmerigoDemo.xcarchive.zip` | 54,949,091 B | 56,867,598 B | −1,918,507 B |

### Unpacked dSYM content (both are a `AmerigoDemo.app.dSYM` bundle)

| File | Cache ON | Cache OFF | Delta |
|---|---|---|---|
| `Contents/Info.plist` | 663 B | 663 B | 0 |
| `Contents/Resources/DWARF/AmerigoDemo` | 111,627,476 B | 116,347,632 B | **−4,720,156 B** (−4.06%) |
| `Contents/Resources/Relocations/aarch64/AmerigoDemo.yml` | 5,940,167 B | 5,947,479 B | −7,312 B |

### `dwarfdump --uuid`

```
Cache ON : UUID: D9E9A465-D2BB-3E1D-8DB8-D3D594B9C979 (arm64)
Cache OFF: UUID: 7EA4FC69-76C3-3787-B143-C5D903907F10 (arm64)
```

UUIDs differ (expected: different source commits).

### `dwarfdump --statistics` highlights (JSON)

| Metric | Cache ON (1671) | Cache OFF (1704) | Delta |
|---|---|---|---|
| `#functions` | 92,682 | 92,828 | −146 |
| `#functions with location` | 31,564 | 31,570 | −6 |
| `#out-of-line functions` | 57,288 | 57,290 | −2 |
| `#inlined functions` | 144,392 | 144,138 | +254 |
| `#unique source variables` | 84,053 | 83,867 | +186 |
| `#source variables` | 121,405 | 120,961 | +444 |
| `#source variables with location` | 93,414 | 93,195 | +219 |
| `#params with binary location` | 35,664 | 35,558 | +106 |
| `#local vars with binary location` | 18,985 | 18,948 | +37 |
| `#bytes in __swift_ast` | 33,931,008 | 33,307,544 | +623,464 |
| `#bytes in __debug_line` | 2,722,135 | 3,653,709 | **−931,574 (−25.5%)** |
| `#bytes in __debug_info` | 10,067,526 | 13,663,248 | **−3,595,722 (−26.3%)** |
| `#bytes in __debug_aranges` | 155,328 | 155,376 | −48 |
| `#bytes in __debug_rnglists` | 544,073 | 540,196 | +3,877 |

The DWARF line-number section and the main DWARF info section are both substantially trimmed under cache-ON. Function counts, inlined-function counts, variable counts are either unchanged or slightly higher on the ON build; the shortfall is almost entirely in `__debug_info` bytes (−3.6 MB) and `__debug_line` bytes (−0.9 MB). These are the sections that encode per-function DIEs (variable location programs, inlined-call-site chains, etc.) — i.e. the exact DWARF data dsymutil would normally pull out of the per-TU object files.

---

## Artifact source-of-truth links

- Full build logs: `/tmp/aci-5540/log-1671.txt` (3186 lines), `/tmp/aci-5540/log-1704.txt` (2516 lines)
- bitrise.yml (identical): `/tmp/aci-5540/bitrise-1671.yml` (and `-1704.yml`)
- Artifact listings: `/tmp/aci-5540/artifacts-1671.json`, `/tmp/aci-5540/artifacts-1704.json`
- Downloaded dSYM zips: `/tmp/aci-5540/dsym-on.zip`, `/tmp/aci-5540/dsym-off.zip`
- Extracted dSYM bundles: `/tmp/aci-5540/dsym-on/`, `/tmp/aci-5540/dsym-off/`
- xcelerate archive log (cache ON): `/tmp/aci-5540/xcelerate-1671.log` (32,559 lines — raw xcodebuild output as deployed to Bitrise artifacts)
- First / last warning blocks (ANSI-stripped): `/tmp/aci-5540/warning-first-5.txt`, `/tmp/aci-5540/warning-last-5.txt`

---

## Slack thread (channel C03NA10MQVC)

**Not read** — this session did not have the `mcp__claude_ai_Slack__*` MCP tools exposed (no slack-cli / token on the host either). Slack thread content is still unknown. Pending: human to paste the Infinum report + any follow-ups, or re-run this investigation with Slack MCP available.

---

## Preliminary findings

**Symptom.** Build 1671 (cache ON, CLI v3.10.0, Xcode 26.6, stack `osx-xcode-26.6.x`): 332 pairs of `warning: 0~<base64>==: No such file or directory` + `note: while processing 0~<base64>==` — raw dsymutil output, printed between `GenerateDSYMFile` and the next step. Archive still succeeds. Build 1704 (cache OFF — workspace entitlement off, activate step reported "not activated in this build" and no-op'd): zero warnings, no cache plumbing in xcodebuild. bitrise.yml byte-identical; stack, Xcode, scheme (`AmerigoDemo`), config (`Release`), `DEBUG_INFORMATION_FORMAT=dwarf-with-dsym`, machine type all match.

**Where.** Only around the dsymutil run on `AmerigoDemo.app/AmerigoDemo`; not swiftc, clang, or linker. Archive-time proxy stats: 989/993 tasks, 4,427/4,431 blobs (4 misses). The 332 "not found" ids are not on the proxy's GET path — dsymutil is doing a *post-link* CAS lookup.

**dSYM delta.** `.dSYM.zip` **−1,926,961 B** cache-on. Unpacked DWARF: `__debug_info` −3,595,722 B (−26.3%), `__debug_line` −931,574 B (−25.5%); function/variable counts unchanged. Per-function debug detail (location programs, inlined-call chains, line tables) is dropped, not whole TUs.

**Build-config identical?** Yes except commit SHA (ticket's "same commit" is wrong — 1671/1704 are different commits on the same `ci/ev-navigation-demo/*` tag) and runtime cache state (workspace entitlement, not a workflow change).

**Top 3 hypotheses** (unproven):

1. **dsymutil has no CAS access.** Compilers emit objects that reference CAS ids (`COMPILATION_CACHE_ENABLE_PLUGIN=YES`, `..._REMOTE_SERVICE_PATH=<proxy sock>`); dsymutil is invoked as plain `/usr/bin/dsymutil` with no plugin / socket / local `COMPILATION_CACHE_CAS_PATH`. Best fit: 332 lookups invisible to the proxy, dSYM green but trimmed.
2. **Apple compile-cache replay drops DWARF.** Replayed objects may ship trimmed `__debug_info`/`__debug_line` regardless of CAS access.
3. **Customer project drift** (Crashlytics "Upload dsyms" script, missing `-g` on a framework). Weak — would also hit cache-off.

Infra / proxy issues (CLI drift, socket races, clock skew, image bumps) not visible: single stack/machine, CLI 3.10.0, 99.9% blob hit rate, archive 1m24s.
