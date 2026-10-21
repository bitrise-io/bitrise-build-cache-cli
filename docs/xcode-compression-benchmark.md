# Xcode cache: zstd blob compression benchmarks (ACI-5600)

Measurements behind the decision on client-side zstd compression of Xcode compilation cache blobs.
Machine for all numbers: Apple M4 Max (14 cores), Xcode 26.4.1. CI VMs are slower, so treat absolute
MB/s as a best case.

## Enabling and opting out

```bash
bitrise-build-cache activate xcode                                       # compression is on by default
bitrise-build-cache activate xcode --remote-cache-compression=false      # opt out
bitrise-build-cache activate xcode --remote-cache-compression-threshold 100
```

On by default (the interactive wizard asks, default yes; `react-native activate` has the same flag).
Compression is a wire-level optimisation: the backend (build-cache-api#819) stores every blob uncompressed under one
key, so compressed and uncompressed clients share entries and nothing goes cold on upgrade. It saves network bytes,
not storage quota.

With it on, the proxy's KV client:
- sends blobs of at least the threshold (default 100 bytes, Bazel's default) as zstd under `kv/zstd/<key>`; smaller
  blobs and ones that do not shrink go as is under `kv/<key>`;
- always requests `kv/zstd/<key>` on download, so the server compresses on the way out;
- computes the upload checksum over the uncompressed bytes and checks the server's hash header on the decoded bytes,
  because the server validates and reports hashes of the uncompressed blob;
- counts offsets in uncompressed bytes like the server: an upload retry re-compresses the blob from the committed offset,
  and a download retry starts over, since a partly received zstd stream cannot be resumed. Because a restart discards
  progress, a download attempt is abandoned only after 60 s without receiving any bytes, not after 60 s in total (the
  uncompressed path keeps its fixed 60 s per attempt).

Ship the backend change to every region first. A server without it reads `kv/zstd/<key>` as the key `blob/zstd`, so every
compressed blob maps to that single key. From the server code (an inference, not a tested path): uploads fail checksum
validation, and a resumed retry can bypass it, since validation is skipped for write offsets above 0. The capabilities
response cannot tell old servers from new ones, so the client has no way to detect this and
`--remote-cache-compression=false` is the only switch.

A config written by an older `activate` has no `remoteCacheCompression` field and keeps compression off until
`activate` runs again.

With compression on, the proxy's blob transfer latency and throughput (`cacheBlobStats`) include compress and
decompress time, while the byte counts are raw.

## 1. Codec microbenchmark

`internal/xcelerate/proxy/compression_bench_test.go`, single-threaded `klauspost/compress` zstd over real
`*.leaf` CAS objects (up to 64 blobs sampled per size bucket, deterministic).

```bash
BITRISE_COMPRESSION_BENCH_CORPUS=<dir containing *.leaf> \
  go test -tags unit ./internal/xcelerate/proxy -run '^$' -bench Zstd
```

Corpus: clean cold build of WordPress iOS (scheme `WordPress`, Swift + clang compile caching on): 15,914 blobs, ~5 GB on disk; the cold build uploaded 3273 MiB (3670 misses). Blobs over 8 MiB are skipped.

| Blob size | Ratio fastest / default / better | Compress MB/s fastest / default / better | Decompress MB/s (default) |
|---|---|---|---|
| 16-256 KiB | 4.57 / 4.81 / 4.94 | 526 / 422 / 356 | ~1100 |
| 256 KiB-4 MiB | 2.41 / 2.55 / 2.59 | 342 / 244 / 195 | ~760 |
| 4-8 MiB (12 blobs) | 2.32 / 2.50 / 2.56 | 402 / 311 / 186 | ~960 |

- No sampled blob grew when compressed, at `default` (first run) and at `fastest`, the shipped level (re-run
  with `BenchmarkZstdRatioBySize`: 0% not smaller in all three buckets).
- `fastest` costs 5-7% of the ratio of `default` (5.0% / 5.5% / 7.2%) and compresses ~25-40% faster (~20-30% less CPU);
  `better` adds 2-3% ratio for 1.2-1.7x the CPU of `default`.
- The corpus has **no blobs under 16 KiB** (no such `.leaf` files exist), so these runs cannot set the
  size threshold. Small entries need to be sampled from real proxy traffic.
- The ticket's "3-4x more data in the same quota" holds for the 16-256 KiB bucket (4.6x at `fastest`); above
  256 KiB the ratio is ~2.3-2.6x. The 4-8 MiB bucket has only 12 blobs and blobs over 8 MiB were not sampled,
  so the share of bytes in the lower-ratio range is not measured.

## 2. Full build through the proxy, with and without compression

`internal/xcelerate/proxy/compression_build_probe_test.go` (`probe` build tag). Builds WordPress iOS with
`/usr/bin/xcodebuild` (not the Bitrise shim) pointed at a real `proxy.Proxy` and `kv.Client` over loopback
gRPC to `fakebackend`. Backend, proxy and xcodebuild are separate processes so their CPU is separable.
In `zstd` mode a decorator between the proxy and `kv.Client` compresses uploads and decompresses
downloads; `off` is a pass-through that only counts bytes. This decorator models the production kv compression
(a marker byte instead of the name prefix, no threshold) and does not exercise the real kv layer; that is covered
by unit tests (the first e2e workflow also asserts the proxy logged compression and that no compressed upload was rejected, unverified until CI runs).

Per mode: a **cold** build fills the backend, then a **warm** build, with DerivedData and the local CAS
wiped, reads everything back from it.

```bash
go test -c -tags probe -o $OUT/probe ./internal/xcelerate/proxy
COMPRESSION_PROBE_WORKSPACE=<WordPress.xcworkspace> COMPRESSION_PROBE_SCHEME=WordPress \
  COMPRESSION_PROBE_OUT=$OUT $OUT/probe -test.run TestCompressionBuildProbe -test.v -test.timeout 0
```

CPU is sampled every 500 ms: system-wide utilisation (all cores, 100 = fully busy; includes the build,
the backend and the proxy) and the proxy process alone (100 = one core).

Caveats:
- Loopback has no bandwidth or latency, so this shows compression's CPU cost without any of its network
  benefit. It is the worst case for compression.
- Modes run back to back, so OS file cache and SPM cache state can favour a later run; the first runs below were `off, zstd`, the ABBA runs (`COMPRESSION_PROBE_MODES=off,zstd,zstd,off`) control for that. Each build's result is also saved to `<out>/<position>-<mode>/<phase>-result.json`, and its build output is deleted afterwards (a full ABBA run otherwise needs ~100 GB).

### Local (M4 Max, 14 cores)

| Mode | Phase | Wall | System CPU% peak / avg / med / p95 | System CPU-s | Proxy CPU% of a core peak / avg / med / p95 | Proxy CPU-s | Wire up / down MiB | Raw up / down MiB |
|---|---|---|---|---|---|---|---|---|
| off | cold | 265.5 s | 100 / 63 / 92 / 100 | 2436 | 61 / 8 / 0 / 29 | 20.9 | 3273 / 0 | 3273 / 0 |
| zstd | cold | 245.0 s | 100 / 69 / 98 / 100 | 2505 | 89 / 12 / 5 / 44 | 29.3 | 1153 / 0 | 3275 / 0 |
| off | warm | 186.4 s | 100 / 45 / 31 / 100 | 1222 | 274 / 9 / 0 / 45 | 16.0 | 712 / 2560 | 712 / 2560 |
| zstd | warm | 171.1 s | 100 / 48 / 34 / 100 | 1190 | 348 / 9 / 0 / 51 | 16.2 | 197 / 956 | 485 / 2791 |

Misses: cold 3670; warm 75 (off) vs 32 (zstd).

Reading it:
- Wire bytes drop 2.8x on upload (3273 to 1153 MiB) and, like for like on the zstd warm run, 2.9x on download
  (2791 raw to 956 wire MiB), in line with the codec benchmark. The `off` warm run downloaded less raw data
  (2560 MiB) because of the different miss counts below.
- Cost in the proxy: +8.4 CPU-s on the cold build (+40%, 20.9 to 29.3), where it compresses every upload;
  proxy p95 rises from 29% to 44% of a core, peak from 61% to 89%. On the warm build, which is mostly
  decompression, the difference is within noise (16.0 vs 16.2 CPU-s).
- Whole-machine CPU is a wash: +2.8% CPU-s cold, -2.6% warm, inside run-to-run noise. The build itself
  dominates (2400 CPU-s vs 20-30 for the proxy).
- Wall time did not get worse: zstd was 8% faster cold and 8% faster warm, but zstd ran second in each
  pair, so this is noise or ordering, not a speedup. Loopback has no network to save, so compression
  cannot be expected to help here; the result only shows it does not hurt on a 14-core machine.
- The two warm builds are not identical workloads (75 vs 32 misses, 712 vs 485 MiB re-uploaded): a
  fraction of keys is not stable between consecutive builds, with or without compression.

### Local ABBA (off, zstd, zstd, off; M4 Max)

Order-controlled rerun: the same cold+warm pair per scenario, run as `off, zstd, zstd, off`
(`COMPRESSION_PROBE_MODES=off,zstd,zstd,off`).

| # | Mode | Phase | Wall | System CPU-s | Proxy CPU-s | Proxy CPU% peak / avg / med / p95 | Wire up / down MiB |
|---|---|---|---|---|---|---|---|
| 0 | off | cold | 263.4 s | 2352 | 21.3 | 62 / 8 / 0 / 30 | 3280 / 0 |
| 0 | off | warm | 184.6 s | 1244 | 15.8 | 263 / 9 / 0 / 50 | 712 / 2567 |
| 1 | zstd | cold | 235.3 s | 2452 | 30.2 | 83 / 13 / 6 / 46 | 1153 / 0 |
| 1 | zstd | warm | 167.4 s | 1171 | 16.7 | 377 / 10 / 0 / 49 | 197 / 956 |
| 2 | zstd | cold | 234.6 s | 2417 | 29.4 | 73 / 13 / 5 / 41 | 1153 / 0 |
| 2 | zstd | warm | 173.2 s | 1233 | 15.5 | 283 / 9 / 0 / 51 | 197 / 956 |
| 3 | off | cold | 248.1 s | 2594 | 21.6 | 69 / 9 / 5 / 29 | 3280 / 0 |
| 3 | off | warm | 184.4 s | 1187 | 15.8 | 300 / 9 / 0 / 49 | 712 / 2567 |

Means of the two runs per mode:

| Phase | Mode | Wall | System CPU-s | Proxy CPU-s | Proxy CPU% peak / avg / p95 |
|---|---|---|---|---|---|
| cold | off | 255.8 s | 2473 | 21.5 | 66 / 8.5 / 29.5 |
| cold | zstd | 235.0 s (-8.1%) | 2435 (-1.6%) | 29.8 (+39%) | 78 / 13 / 43.5 |
| warm | off | 184.5 s | 1216 | 15.8 | 282 / 9 / 49.5 |
| warm | zstd | 170.3 s (-7.7%) | 1202 (-1.1%) | 16.1 (+2%) | 330 / 9.5 / 50 |

Reading it:
- The A-B-B-A order did not remove the wall-time gap: both `zstd` cold runs (235 s) beat both `off` cold runs (248, 263 s),
  and the same holds warm (167-173 s vs 184-185 s). Ordering alone does not explain it. The cause is not
  isolated; fewer bytes through the in-memory loopback backend and gRPC is the likely reason, and no real network
  is involved, so this is not a measure of what compression saves on a real link.
- System CPU-s is within run-to-run noise (the two `off` cold runs differ by 10%).
- Proxy cost is as before: +39% CPU-s on cold uploads (p95 29.5% to 43.5% of a core), no real change on warm builds.

### RDE (6 cores, 14 GB, Xcode 26.6)

| Mode | Phase | Wall | System CPU% peak / avg / med / p95 | System CPU-s | Proxy CPU% of a core peak / avg / med / p95 | Proxy CPU-s | Wire up / down MiB | Raw up / down MiB | Misses |
|---|---|---|---|---|---|---|---|---|---|
| off | cold | 509.5 s | 100 / 77 / 98 / 100 | 2416 | 73 / 5 / 2 / 17 | 23.4 | 3047 / 0 | 3047 / 0 | 2980 |
| zstd | cold | 462.7 s | 100 / 76 / 99 / 100 | 2175 | 83 / 7 / 2 / 26 | 31.3 | 1149 / 0 | 3049 / 0 | 2980 |
| off | warm | 364.0 s | 100 / 60 / 69 / 99 | 1331 | 195 / 8 / 0 / 49 | 30.5 | 711 / 2336 | 711 / 2336 | 75 |
| zstd | warm | 281.9 s | 100 / 60 / 64 / 100 | 1028 | 189 / 9 / 0 / 58 | 24.2 | 197 / 952 | 483 / 2566 | 32 |

Reading it:
- Wire bytes drop 2.7x on upload (3047 to 1149 MiB) and, like for like on the zstd warm run, 2.7x on download (2566 raw to 952).
- Proxy CPU follows the local pattern on the cold build (+7.9 CPU-s, +34%; p95 17% to 26% of a core). On the warm
  build it went down (30.5 to 24.2 CPU-s): the proxy hashes and copies every wire byte, which costs more than
  decoding zstd at these ratios.
- Wall time and system CPU-s fell 9% and 10% cold, 23% and 23% warm. Do not read this as a compression speedup:
  the VM had just booted when the first (off cold) build started, `off` always ran before `zstd`, and
  the 6 cores were saturated (system CPU p95 100%) so run-to-run noise is large. The result shows
  compression did not slow a CPU-saturated 6-core machine; a controlled A/B (ABBA order, repeated) is needed to
  quantify any gain.
- As locally, the warm builds differ in workload (75 vs 32 misses, 711 vs 483 MiB re-uploaded).

### RDE ABBA (off, zstd, zstd, off; 6 cores)

| # | Mode | Phase | Wall | System CPU-s | Proxy CPU-s | Proxy CPU% peak / avg / med / p95 | Wire up / down MiB | Raw up / down MiB |
|---|---|---|---|---|---|---|---|---|
| 0 | off | cold | 557.5 s | 2653 | 25.9 | 63 / 5 / 1 / 18 | 3054 / 0 | 3054 / 0 |
| 0 | off | warm | 293.6 s | 1072 | 20.0 | 148 / 7 / 0 / 34 | 711 / 2343 | 711 / 2343 |
| 1 | zstd | cold | 451.6 s | 2201 | 33.5 | 102 / 7 / 3 / 28 | 1149 / 0 | 3056 / 0 |
| 1 | zstd | warm | 279.0 s | 1042 | 24.3 | 167 / 9 / 0 / 56 | 197 / 952 | 483 / 2573 |
| 2 | zstd | cold | 387.6 s | 1846 | 28.2 | 58 / 7 / 3 / 28 | 1149 / 0 | 3056 / 0 |
| 2 | zstd | warm | 277.4 s | 1057 | 31.2 | 175 / 11 / 0 / 82 | 197 / 952 | 483 / 2573 |
| 3 | off | cold | 464.2 s | 2214 | 22.6 | 69 / 5 / 2 / 18 | 3054 / 0 | 3054 / 0 |
| 3 | off | warm | 276.9 s | 1011 | 21.5 | 154 / 8 / 0 / 41 | 711 / 2343 | 711 / 2343 |

Means of the two runs per mode:

| Phase | Mode | Wall | System CPU-s | Proxy CPU-s | Proxy CPU% peak / avg / p95 |
|---|---|---|---|---|---|
| cold | off | 510.9 s | 2434 | 24.2 | 66 / 4.8 / 18 |
| cold | zstd | 419.6 s (-17.9%) | 2024 (-16.8%) | 30.9 (+27%) | 80 / 7.4 / 28 |
| warm | off | 285.3 s | 1042 | 20.7 | 151 / 7.3 / 38 |
| warm | zstd | 278.2 s (-2.5%) | 1050 (+0.8%) | 27.8 (+34%) | 171 / 10 / 69 |

Reading it:
- The RDE is much noisier than the laptop: the two `off` cold runs differ by 17% (557 s and 464 s; the first ran on a
  freshly wiped VM) and the two `zstd` cold runs by 14%. The -18% cold wall time and -17% cold CPU-s are therefore not a
  measured speedup; against the second `off` run alone it is about -10%. Warm wall time is flat (-2.5%, inside the noise).
- Wire bytes drop 2.7x on upload (3056 raw to 1149 wire MiB) and 2.7x on download (2573 raw to 952 wire MiB), as on the laptop.
- The proxy's CPU cost shows up more on the 6 cores: +27% CPU-s on cold uploads, and p95 18% to 28% of a core. On
  warm builds it was +34% here, but the one earlier A-B run on this machine showed the opposite (30.5 to 24.2 CPU-s) and
  the local runs show no change, so treat the warm proxy CPU difference as unresolved.
- Across all runs, compression did not slow the build on either machine and did not raise whole-machine CPU-s beyond noise.
