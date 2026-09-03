# Network endpoints (firewall allowlist)

Every Bitrise-owned host the CLI, the Gradle plugins it installs, the Bazel config it writes, and the Xcode/ccache proxies talk to. Use this to build a firewall allowlist.

Non-Bitrise hosts are out of scope here: the Gradle init script also resolves dependencies from the Gradle Plugin Portal, Maven Central and JitPack, and installing/updating the CLI itself goes to GitHub and Google Artifact Registry (see [`docs/install.md`](install.md)).

## Data plane — hit on every build

| Host | Port | Protocol | Used by |
|---|---|---|---|
| `bitrise-accelerate.services.bitrise.io` | 443 | gRPC/TLS | Gradle remote build cache, Bazel remote cache + RBE, Xcode (xcelerate proxy, XCC/Swift cache), ccache storage helper, Gradle Test Distribution |
| `gradle-analytics.services.bitrise.io` | 443 **and 444** | gRPC/TLS | Gradle plugin analytics |
| `gradle-sink.services.bitrise.io` | 443 | HTTPS | Gradle analytics HTTP sink |
| `xcode-analytics.services.bitrise.io` | 443 | HTTPS | Xcode invocation analytics, DerivedData save/restore |
| `multiplatform-analytics.services.bitrise.io` | 443 | HTTPS | ccache, React Native and `xcodebuild` invocation analytics |
| `flare-bes.services.bitrise.io` | 443 | gRPC/TLS | Bazel Build Event Service |
| `repository-manager.services.bitrise.io` | **8090** | HTTPS | Maven mirror — only when `activate gradle-mirrors` is used |

## Control plane — activation and login

| Host | Port | Purpose |
|---|---|---|
| `app.bitrise.io` | 443 | Benchmark-phase query (`/build-cache/{workspace}/invocations/{tool}/command_benchmark_status`), invocation links, OAuth client metadata, `/oidc/token` |
| `oauth.bitrise.io` | 443 | OAuth issuer for `bitrise-build-cache auth login` |
| `api.bitrise.io` | 443 | `/v0.1/organizations` — workspace picker during login |

## Allowlist

```
*.services.bitrise.io    :443, :444, :8090
app.bitrise.io           :443
oauth.bitrise.io         :443
api.bitrise.io           :443
```

Drop `oauth.bitrise.io` and `api.bitrise.io` if you authenticate with `BITRISE_BUILD_CACHE_AUTH_TOKEN` instead of interactive login.

## Gotchas

- **Port 444 on `gradle-analytics`.** A 443-only rule silently breaks Gradle analytics while the build cache itself keeps working.
- **Port 8090 on `repository-manager`.** Non-standard, and the mirror activation soft-fails, so a blocked port shows up as Maven Central traffic bypassing the proxy rather than as a build error.
- **DC-local resolution on Bitrise VMs.** `bitrise-accelerate` and `repository-manager` are redirected to datacenter-internal IPs via `/etc/hosts` written by the preboot reconciler, so on Bitrise-managed VMs that traffic never leaves the datacenter. The public names matter for self-hosted or BYO runners.
- **Overrides.** The cache and RBE endpoints can be repointed at a proxy with `BITRISE_BUILD_CACHE_ENDPOINT` and `BITRISE_RBE_ENDPOINT`.
