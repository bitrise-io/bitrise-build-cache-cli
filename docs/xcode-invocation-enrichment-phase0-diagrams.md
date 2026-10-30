# Phase 0 — Class + Flow Diagrams

Companion to `xcode-invocation-enrichment-plan-2026-10-30.md` (rev3). Mermaid; render in any MD viewer that supports it (VS Code preview, GitHub, etc.).

## 1. Package map

```mermaid
flowchart LR
    subgraph cmd/xcode
        WRAP[xcodebuild.go<br/>attachEnrichment]
        HOOK[enrichment_hook.go<br/>newEnrichmentHook]
        PROXY[xcelerate_start_proxy.go<br/>wires hook into Enricher]
    end

    subgraph internal/xcelerate/enrichment
        ENR[enricher.go<br/>EnrichmentHook field]
    end

    subgraph internal/xcelerate/xcprobe
        AGG[aggregator.go<br/>Run / runWith]
        subgraph collect
            CIF[Collector interface]
            HSC[hostsnap]
            BTC[buildtiming]
            BSC[buildsettings]
        end
        subgraph host
            SNAP[Snapshot<br/>embeds common.HostMetadata]
        end
        subgraph payload
            PL[EnrichmentPayload]
            EM[AttachToInvocation]
        end
    end

    subgraph internal/config/xcelerate
        OPT[EnrichmentOptions<br/>LoadEnrichmentOptions]
    end

    subgraph internal/config/common
        HM[HostMetadata<br/>unchanged]
        BPP[BenchmarkPhaseProvider<br/>unchanged]
    end

    subgraph internal/xcelerate/xcodebuildinfo
        XCI[Client.ShowBuildSettings]
    end

    subgraph internal/xcelerate/analytics
        INV["Invocation<br/>+Enrichment json.RawMessage"]
    end

    subgraph internal/xcelerate/deriveddata
        DD[DDRootFromManifestPath]
    end

    WRAP --> AGG
    WRAP --> EM
    WRAP --> OPT
    HOOK --> AGG
    HOOK --> EM
    HOOK --> OPT
    HOOK --> DD
    PROXY --> ENR
    PROXY --> HOOK
    ENR -.calls hook fn.-> HOOK
    AGG --> CIF
    AGG --> PL
    CIF --> HSC
    CIF --> BTC
    CIF --> BSC
    HSC --> SNAP
    BSC --> XCI
    SNAP --> HM
    EM --> INV
    AGG --> BPP
```

Dependency rules enforced by `make lint-arch`:
- `collect/` imports no config package.
- `payload/` imports `analytics` one-way (field is `json.RawMessage`).
- `cmd/xcode` imports `aggregator` + `config/xcelerate`, nothing deeper.

## 2. Type relationships (class diagram)

```mermaid
classDiagram
    class Collector {
        <<interface>>
        +Name() string
        +Collect(ctx, Input) (Output, error)
    }

    class Input {
        +DerivedDataPath string
        +Project string
        +Scheme string
        +WindowStart time.Time
        +WindowEnd time.Time
        +XcodebuildInfo *xcodebuildinfo.Client
        +XcodebuildInfoBytes []byte
        +HostBase common.HostMetadata
        +BenchmarkPhase common.BenchmarkPhase
    }

    class Output {
        +Host *host.Snapshot
        +BuildTimings *BuildTimingHistogram
        +BuildSettings *BuildSettingsSnapshot
    }

    class HostCollector
    class BuildTimingCollector
    class BuildSettingsCollector

    Collector <|.. HostCollector
    Collector <|.. BuildTimingCollector
    Collector <|.. BuildSettingsCollector

    class Snapshot {
        +common.HostMetadata «embedded»
        +Thermal *ThermalInfo
        +MemFreeMB uint64
        +SwapTotalMB uint64
        +SwapFreeMB uint64
        +UptimeSeconds uint64
        +IsVirtual bool
        +SpotlightActive bool
        +TimeMachineActive bool
        +ModelID string
        +Chip string
    }

    class HostMetadata {
        +OS, CPUCores, MemSize...
        «unchanged, Bazel-scoped»
    }

    Snapshot --|> HostMetadata : embeds

    class EnrichmentPayload {
        +SchemaVersion int
        +CollectorErrors map~string, string~
        +Host *host.Snapshot
        +BuildSettings *BuildSettingsSnapshot
        +BuildTimings *BuildTimingHistogram
    }

    class EnrichmentOptions {
        +HostSnapshot bool
        +BuildTimingSummary bool
        +BuildSettings bool
        +PerCollectorTimeout time.Duration
        +FailOpen bool
        +AnyEnabled() bool
    }

    class Invocation {
        +...existing fields...
        +Enrichment json.RawMessage
    }

    HostCollector ..> Snapshot
    BuildSettingsCollector ..> BuildSettingsSnapshot
    BuildTimingCollector ..> BuildTimingHistogram
    EnrichmentPayload --> Snapshot : Host
    EnrichmentPayload --> BuildTimingHistogram : BuildTimings
    EnrichmentPayload --> BuildSettingsSnapshot : BuildSettings
    EnrichmentPayload ..> Invocation : AttachToInvocation sets Enrichment
```

## 3. Aggregator fan-out

```mermaid
sequenceDiagram
    participant Caller as Wrapper / IDE hook
    participant Agg as aggregator.Run
    participant HC as hostsnap
    participant BT as buildtiming
    participant BS as buildsettings
    participant Pay as payload.AttachToInvocation
    participant Inv as analytics.Invocation

    Caller->>Agg: Run(ctx, opts, in)
    Note over Agg: AnyEnabled? false → return empty
    Agg->>Agg: build enabled-set from opts
    par
        Agg->>HC: Collect(ctx+timeout, in)
        HC-->>Agg: Output{Host}
    and
        Agg->>BT: Collect(ctx+timeout, in)
        BT-->>Agg: Output{BuildTimings} or err
    and
        Agg->>BS: Collect(ctx+timeout, in)
        BS-->>Agg: Output{BuildSettings} or err
    end
    Agg->>Agg: merge Outputs<br/>errors → CollectorErrors[name]
    Agg-->>Caller: EnrichmentPayload
    Caller->>Pay: AttachToInvocation(inv, p)
    Note over Pay: marshal payload<br/>if >64 KB drop largest sub-row<br/>marshal once more
    Pay->>Inv: Enrichment = json.RawMessage(bytes)
```

## 4. Opt-in config resolution

```mermaid
flowchart TD
    FILE["~/.bitrise-xcelerate/config.json<br/>enrichment{}"] -->|lowest precedence| MERGE
    ENV["BITRISE_BUILD_CACHE_ENRICH=hostsnap,buildtiming,...<br/>BITRISE_BUILD_CACHE_ENRICH_ALL=1<br/>BITRISE_BUILD_CACHE_ENRICH_NONE=1"] -->|mid| MERGE
    CLI["--enrich=hostsnap,...<br/>--enrich-all<br/>--enrich-none"] -->|highest| MERGE
    MERGE[LoadEnrichmentOptions] --> OPTS[EnrichmentOptions]
    OPTS --> AE{AnyEnabled?}
    AE -->|no| SHORT[skip aggregator → zero cost]
    AE -->|yes| RUN[aggregator.Run]
```

`--enrich-none` beats everything else at every tier.

## 5. Two hook sites (wrapper vs IDE)

```mermaid
flowchart TB
    subgraph wrapperPath [Wrapper path  cmd/xcode/xcodebuild.go]
        W1[xcodebuild returns] --> W2{opts.AnyEnabled?}
        W2 -->|no| W3[saveInvocationAndRelation]
        W2 -->|yes| W4[attachEnrichment:<br/>build Input, Run, Attach]
        W4 --> W3
    end

    subgraph ideFlow [IDE path  internal/xcelerate/enrichment/enricher.go]
        I1[group end detected] --> I2[attachXcresultSummary]
        I2 --> I3{EnrichmentHook set &&<br/>opts.AnyEnabled?}
        I3 -->|no| I4[PutInvocation inv]
        I3 -->|yes| I5[newEnrichmentHook:<br/>build Input via DDRootFromManifestPath,<br/>Run, Attach]
        I5 --> I4
    end

    PROXY[xcelerate_start_proxy.go] -->|wires at startup| I3
    SYM[Guard symmetric per architect carry-forward]
    SYM -.-> W2
    SYM -.-> I3
```

Both hooks share: same aggregator, same payload type, same AnyEnabled guard, same Attach. Only the Input-construction code differs.

## 6. Collector-error isolation

```mermaid
sequenceDiagram
    participant Agg as runWith
    participant Good1 as hostsnap (OK)
    participant Bad as broken (err)
    participant Good2 as buildsettings (OK)
    participant Pay as EnrichmentPayload

    par
        Agg->>Good1: Collect
        Good1-->>Agg: Output{Host}
    and
        Agg->>Bad: Collect
        Bad-->>Agg: nil, errors.New("boom")
    and
        Agg->>Good2: Collect
        Good2-->>Agg: Output{BuildSettings}
    end
    Agg->>Pay: merge
    Note over Pay: Host populated<br/>BuildSettings populated<br/>CollectorErrors["broken"] = "boom"
```

One failure never kills siblings. `FailOpen=true` is the default.

## 7. Oversize-payload soft cap

```mermaid
flowchart LR
    IN[EnrichmentPayload] --> M1[json.Marshal]
    M1 --> S1{size ≤ SoftCapBytes 64 KB?}
    S1 -->|yes| OUT[inv.Enrichment = bytes]
    S1 -->|no| DROP[drop largest sub-row<br/>Host / BuildTimings / BuildSettings]
    DROP --> M2[json.Marshal]
    M2 --> OUT
    Note1[Contract: never fails solely on size<br/>Post-drop MAY still exceed cap<br/>intentional single-pass]
```

Single-pass by design; a future refactor that assumes "post-drop ≤ cap" breaks the contract — the `emit_test.go` fixture locks this in.

## 8. Phase-0 non-goals (visual reminder)

```mermaid
flowchart LR
    classDef out fill:#333,stroke:#999,color:#aaa
    SLF[SLF parser]:::out
    SBS[sibling PutEnrichment stream]:::out
    FI[flag injection into user build]:::out
    STATS[swift -stats-output-dir]:::out
    CTT[clang -ftime-trace]:::out
    LDS[-Xlinker -print_statistics]:::out
    BDB[build.db SQLite reader]:::out
    XAL[xcactivitylog SLF collector]:::out

    PHASE0[Phase 0 scope] --> HOSTSNAP
    PHASE0 --> BT[buildtiming]
    PHASE0 --> BS[buildsettings]
    PHASE0 --> OPTS2[EnrichmentOptions]
    PHASE0 --> HOOKS[Wrapper + IDE hooks]
    PHASE0 --> NESTED[Nested json.RawMessage on Invocation]

    PHASE0 -.-> SLF
    PHASE0 -.-> SBS
    PHASE0 -.-> FI
    PHASE0 -.-> STATS
    PHASE0 -.-> CTT
    PHASE0 -.-> LDS
    PHASE0 -.-> BDB
    PHASE0 -.-> XAL
```

Grey nodes are out of Phase 0 — explicitly deferred to Phase 1-4 per the plan doc.
