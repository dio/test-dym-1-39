# Build Brief: envoy-builder-mini macOS arm64

## Goal

Build an Envoy binary for macOS arm64 from source that includes the
`envoy.filters.http.dynamic_modules` HTTP filter extension. The archive-envoy
nightly for 1.39-dev is missing this extension, which breaks all Go dynamic
module filters that call `DefineCounter`/`DefineGauge`/`DefineHistogram` during
`on_http_filter_config_new`.

## Target commit

```
5d71bc2fcf995dd052fd9ace4c848d63217de687
```

This is the HEAD of envoyproxy/envoy main as of 2026-05-20 and matches the
1.39-dev nightly binary at tetratelabs/archive-envoy (tag `dev`).

Reference: 1.38.0 release is `f1dd21b16c244bda00edfb5ffce577e12d0d2ec2`.

## Required extension

`//source/extensions/filters/http/dynamic_modules:config`

This extension is registered in the default
`source/extensions/extensions_build_config.bzl` — a stock source build at this
commit includes it automatically. No extra Bazel flags are needed to pull it in.
The archive-envoy nightly excluded it via their own build script; that is the
root cause, not a source-level omission.

## Bazel build invocation (macOS arm64)

```bash
bazel build \
  -c opt \
  --config=macos \
  --strip=always \
  --jobs=HOST_CPUS \
  --show_progress_rate_limit=15 \
  //source/exe:envoy
```

Key points:
- `--config=release` does NOT exist in Envoy's .bazelrc — do not use it.
- `--//:contrib_enabled=false` does NOT exist in envoyproxy/envoy main — do not use it.
- `-c opt` is the compilation mode (optimized, -O2).
- `--config=macos` sets PATH, tcmalloc=disabled, and macOS-specific compiler flags.
- `--strip=always` strips DWARF from the output binary (~2x size reduction).

To discover what configs are actually defined at a given commit:

```bash
grep -E "^build:" .bazelrc | cut -d" " -f1 | sort -u
```

## Verification (mandatory before declaring success)

```bash
# Must return exactly one line. Zero = extension missing, build is broken.
nm -g envoy | grep "_envoy_dynamic_module_callback_http_filter_config_define_counter"

# Sanity check: expect >50 http_filter callback symbols.
nm -g envoy | grep "envoy_dynamic_module_callback_http_filter" | wc -l
```

Note: macOS nm output prefixes symbols with an underscore (`_envoy_dynamic_...`).
Linux does not. The grep pattern above matches both because it targets the suffix.

Reference symbol counts:
- 1.38.0 macOS build: 674 exported `envoy_dynamic*` symbols
- 1.39-dev archive-envoy nightly: 22 (dns_resolver + transport_socket only, no http_filter)
- Expected from this build: ≥ 674 http_filter symbols present

## Repository

```
https://github.com/dio/test-dym-1-39
```

The reproducer lives here. After the builder produces the binary, set
`ENVOY_139_BIN` and run:

```bash
make build
ENVOY_139_BIN=/path/to/envoy make run-139-crash
```

`ENVOY_139_BIN` overrides the default download path for the 1.39-dev target.
Do NOT use bare `ENVOY_BIN=` on `make run-139-crash` — that variable is set
unconditionally inside the recipe and any external assignment is silently ignored.

### Expected outcomes

**If the crash is purely due to the missing symbols (null function pointer):**

```
=== 1.39-dev crash ===
UNEXPECTED: 1.39-dev did not crash: bug may be fixed!
```
Both TestSafeFilter and TestCrashFilter PASS. The fix is confirmed.

**If the crash persists on a full build (nested CGo is independently broken):**

```
=== 1.39-dev crash ===
EXPECTED: 1.39-dev crashed (exit 2): bug confirmed
```
TestSafeFilter passes but TestCrashFilter still crashes. The symbol issue and
the nested CGo signal-forwarding issue (`runtime.sigfwdgo` at 0x0) are separate
bugs. File against Go/CGo signal handling on macOS arm64 with 1.39-dev Envoy.

## Background (why the nightly is broken)

The archive-envoy macOS nightly omits the HTTP dynamic modules extension from
its build target. The source-level `bazel/exported_symbols_apple.txt` already
has `_envoy_dynamic_module_callback_*` — the symbol export config is correct.
The problem is purely the extension not being compiled in.

1.38.0 macOS build: 674 exported `envoy_dynamic*` symbols.
1.39-dev nightly:    22 exported `envoy_dynamic*` symbols (dns_resolver +
                     transport_socket only — no http_filter at all).
