# test-dym-1-39

Minimal reproducer for the Envoy 1.39-dev segfault when a Go dynamic module
calls back into Envoy (DefineCounter, DefineGauge, DefineHistogram, or any log
callback) during `on_http_filter_config_new`.

Uses the upstream Go SDK (`github.com/envoyproxy/envoy/source/extensions/dynamic_modules`).

## The bug

On macOS arm64, 1.39-dev crashes immediately after "ABI version v0.1.0 matched"
when any factory function invokes a C callback during config construction:

```
Dynamic module ABI version v0.1.0 matched.
Caught Segmentation fault: 11, suspect faulting address 0x0
#0: runtime.sigfwdgo [0x...]
```

The same dynamic module code works on Linux (amd64 and arm64, both 1.38.0 and 1.39-dev).
The bug is macOS-specific; Linux arm64 is unaffected, ruling out the arch.

The crash is `runtime.sigfwdgo` jumping to address `0x0`: Go's `fwdSig[SIGSEGV]`
contains a null pointer at the point Envoy calls `on_http_filter_config_new`. On
macOS arm64, Go's `getsig()` reads the existing `sigaction` before installing its
own handler; in 1.39-dev that read returns null when the `.so` is loaded via
`dlopen` and a config callback fires. On Linux amd64 the signal plumbing is in
place before any config callback, so nested CGo works.

## Two filters

- `safe-filter`: no C callback in `Create`. Passes everywhere.
- `crash-filter`: calls `h.DefineCounter("test_requests", "status")` in `Create`.
  Crashes on macOS arm64 + 1.39-dev. Works on Linux (amd64 and arm64) on both versions.

## CI matrix

| Platform | 1.38.0 | 1.39-dev safe | 1.39-dev crash |
|---|---|---|---|
| macOS arm64 | PASS | PASS | SEGFAULT (confirmed) |
| Linux amd64 | PASS | PASS | PASS (not affected) |
| Linux arm64 | PASS | PASS | PASS (not affected) |

Linux arm64 passes, so the bug is **macOS-specific**, not arm64-specific.
The differentiating factor is the OS, not the architecture.

## Run

All Envoy binaries are downloaded into `.bin/` on first use. No external
dependencies required.

```
make run-138          # downloads 1.38.0 -> .bin/envoy-138/envoy; safe=PASS crash=PASS
make run-139-safe     # downloads 1.39-dev -> .bin/envoy-139/envoy; safe=PASS (safe-only config)
make run-139-crash    # 1.39-dev, full config; SEGFAULT on macOS arm64, PASS on Linux amd64
```

Run the full sequence:

```
make
```

## Binaries

| Path | Version | Source |
|---|---|---|
| `.bin/envoy-138/envoy` | 1.38.0 | tetratelabs/archive-envoy `v1.38.0` |
| `.bin/envoy-139/envoy` | 1.39.0-dev | tetratelabs/archive-envoy `dev` tag (nightly) |

Both are downloaded via `curl` from archive-envoy releases. The `dev` tag always
points to the latest nightly; re-running `make clean` and then `make` will pick up
a newer build.

## Cross-compile for Linux

`build-linux` cross-compiles the `.so` for linux/amd64 using zig cc. Zig is
downloaded automatically into `.bin/zig-dist/` via `scripts/zigcc.sh`.

```
make build-linux   # produces libtestdym-linux-amd64.so
```

## Root cause

Any nested CGo call (C to Go to C) made during `on_http_filter_config_new` triggers
the crash on macOS arm64 + 1.39-dev. The `on_program_init` boundary is safe.

Fix options:
1. File upstream Envoy bug: signal handler state must be stable on all platforms
   before config callbacks fire.
2. Defer metric definition to first use (lazy init, sync.Once in factory).
