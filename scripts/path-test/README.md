# PATH-toolprovider repro (ACI-5140)

Two minimal Bitrise yml configs proving that the top-level `tools:` declaration is what causes step-set `PATH` to be wiped at every utility-workflow (`before_run` / `after_run`) boundary.

## Layout

Both ymls run the same workflow chain: `chain1 → chain2 → chained`.

* `chain1` calls `set-env-var@0` to append `TESTPATH` to `PATH`, then echoes `PATH` and asserts `TESTPATH` is present.
* `chain2` echoes `PATH` and asserts `TESTPATH` is present.
* `chained` (main) echoes `PATH` and asserts `TESTPATH` is present.

Each script step exits non-zero if `TESTPATH` is missing, so the build status reflects the assertion.

## Files

* `no-tools.yml` — no top-level `tools:` block. Expected: **all three assertions pass, build green.** `set-env-var`'s PATH propagates across every workflow boundary.
* `with-tools.yml` — adds `tools: { golang: 1.26:latest }`. Expected: **chain1 passes, chain2 fails (`TESTPATH MISSING`)**. The toolprovider re-injects `PATH = os.Getenv("PATH") + tool-paths` at every workflow boundary (`bitrise-io/bitrise` `cli/run.go:338-352` + `toolprovider/env.go:14,35`); last-write-wins via `envman/env/expand.go:121` clobbers `set-env-var`'s PATH at the chain1→chain2 boundary.

## Trigger from Bitrise

Use a custom config trigger (replace `<APP_SLUG>` and `<BUILD_TOKEN>`):

```bash
curl -X POST "https://app.bitrise.io/app/<APP_SLUG>/build/start.json" \
  -H "Content-Type: application/json" \
  -d @- <<EOF
{
  "hook_info": {"type": "bitrise", "build_trigger_token": "<BUILD_TOKEN>"},
  "build_params": {
    "branch": "aci-5140-path-toolprovider-repro",
    "workflow_id": "chained",
    "bitrise_yml_path": "scripts/path-test/no-tools.yml"
  }
}
EOF
```

Repeat with `"bitrise_yml_path": "scripts/path-test/with-tools.yml"`.

Or locally:

```bash
bitrise run --config scripts/path-test/no-tools.yml chained
bitrise run --config scripts/path-test/with-tools.yml chained
```
