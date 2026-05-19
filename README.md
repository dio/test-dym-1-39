# test-dym-1-39

Minimal reproducer for the Envoy 1.39-dev segfault when a Go dynamic module
calls back into Envoy (DefineCounter, DefineGauge, DefineHistogram, or any log
callback) during `on_http_filter_config_new`.

Uses the upstream Go SDK (`github.com/envoyproxy/envoy/source/extensions/dynamic_modules`).

## The bug

On macOS, 1.39-dev crashes immediately after "ABI version v0.1.0 matched" when
any factory function invokes a C callback during config construction:

```
Dynamic module ABI version v0.1.0 matched.
Caught Segmentation fault: 11, suspect faulting address 0x0
#0: runtime.sigfwdgo [0x...]
```

1.38.0 handles the same .so without issues.

The crash is `runtime.sigfwdgo` jumping to address `0x0`: Go's `fwdSig[SIGSEGV]`
contains a null pointer at the point Envoy calls `on_http_filter_config_new`.
Envoy's signal handler is not fully set up before dynamic module config callbacks
fire on 1.39-dev.

## Two filters

- `safe-filter`: no C callback in `Create`. Passes on both versions.
- `crash-filter`: calls `h.DefineCounter("test_requests", "status")` in `Create`.
  Passes on 1.38.0, crashes 1.39-dev.

## Run

```
make run-138   # both PASS
make run-139   # safe=PASS, crash=SEGFAULT (downloads 1.39-dev automatically)
```

Or with an existing 1.39-dev binary:

```
ENVOY_BIN=/tmp/envoy-dev/envoy go test -v -count=1 -timeout 30s ./...
```

## Cross-compile for Linux

The crash is macOS-specific. To reproduce on Linux, cross-compile the .so with zig cc:

```
make build-linux   # produces libtestdym-linux-amd64.so
# copy to a Linux host, run against ARCHIVE_ENVOY_OS=linux ARCHIVE_ENVOY_ARCH=amd64
```

## Root cause

Any nested CGo call (C to Go to C) made during `on_http_filter_config_new` triggers
the crash. The `on_program_init` boundary is safe; the failure is specific to
`on_*_config_new` entrypoints on 1.39-dev macOS.

Fix options:
1. File upstream Envoy bug: signal handler state must be stable before config
   callbacks fire.
2. Defer metric definition to first use (lazy init, sync.Once in factory).
