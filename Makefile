ENVOY_138_VERSION := 1.38.0
ENVOY_DEV_TAG     := dev

ZIG_VERSION := 0.16.0
ZIG_BIN     := $(CURDIR)/.bin/zig-dist/zig

# Detect host OS and arch (Go-style: darwin/linux, amd64/arm64).
GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# archive-envoy asset names use "v<ver>" prefix for stable, "dev" for nightly.
ENVOY_138     := .bin/envoy-138/envoy
ENVOY_138_URL := https://github.com/tetratelabs/archive-envoy/releases/download/v$(ENVOY_138_VERSION)/envoy-v$(ENVOY_138_VERSION)-$(GOOS)-$(GOARCH).tar.xz

ENVOY_139     := .bin/envoy-139/envoy
ENVOY_139_URL := https://github.com/tetratelabs/archive-envoy/releases/download/$(ENVOY_DEV_TAG)/envoy-$(ENVOY_DEV_TAG)-$(GOOS)-$(GOARCH).tar.xz

ZIGCC := $(CURDIR)/scripts/zigcc.sh

# Native .so (current OS/arch).
SO = libtestdym.so

# Linux cross-compiled .so.
SO_LINUX = libtestdym-linux-amd64.so

.PHONY: all build build-linux clean run-138 run-139-safe run-139-crash kill-stale

all: run-138 run-139-safe run-139-crash

build: $(SO)

# Build native .so.
$(SO): filter/main.go go.mod
	CGO_ENABLED=1 go build -trimpath -buildmode=c-shared -o $(SO) ./filter

# Cross-compile for linux/amd64 via zig cc (works on macOS and Linux hosts).
build-linux: $(ZIG_BIN)
	TARGET=x86_64-linux-gnu CGO_ENABLED=1 CC=$(ZIGCC) GOOS=linux GOARCH=amd64 \
	  go build -trimpath -buildmode=c-shared -o $(SO_LINUX) ./filter

# Download zig on demand (auto-detects host OS/arch, self-contained in zigcc.sh).
$(ZIG_BIN):
	@ZIG_VERSION=$(ZIG_VERSION) bash $(ZIGCC) true 2>/dev/null || true

# Download Envoy 1.38.0 on demand.
$(ENVOY_138):
	@echo "downloading envoy $(ENVOY_138_VERSION) ($(GOOS)/$(GOARCH)) ..."
	@mkdir -p .bin/envoy-138
	curl -fsSL "$(ENVOY_138_URL)" \
	  | tar -xJ -C .bin/envoy-138 --strip-components=2 \
	      "envoy-v$(ENVOY_138_VERSION)-$(GOOS)-$(GOARCH)/bin/envoy"
	@chmod +x $(ENVOY_138)
	@$(ENVOY_138) --version

# Download Envoy 1.39-dev on demand.
$(ENVOY_139):
	@echo "downloading envoy $(ENVOY_DEV_TAG) ($(GOOS)/$(GOARCH)) ..."
	@mkdir -p .bin/envoy-139
	curl -fsSL "$(ENVOY_139_URL)" \
	  | tar -xJ -C .bin/envoy-139 --strip-components=2 \
	      "envoy-$(ENVOY_DEV_TAG)-$(GOOS)-$(GOARCH)/bin/envoy"
	@chmod +x $(ENVOY_139)
	@$(ENVOY_139) --version

# Kill stale Envoy processes holding our test ports.
# Pattern targets our .bin/envoy-NNN/envoy paths specifically.
# Avoid patterns that appear in the pkill argv itself: on Linux pkill -f
# matches /proc/PID/cmdline, so a pattern like "envoy.*envoy.yaml" matches
# the shell running pkill (which has that literal string in its cmdline),
# killing the parent make shell and causing "Terminated" with exit 2.
kill-stale:
	@pkill -f "\.bin/envoy-[0-9]" 2>/dev/null || true
	@sleep 0.5

# 1.38.0: both safe-filter and crash-filter PASS.
# TestMain kills envoy on exit; no inline pkill needed.
run-138: build $(ENVOY_138) kill-stale
	@echo "=== 1.38.0 (expect: safe=PASS crash=PASS) ==="
	ENVOY_BIN=$(CURDIR)/$(ENVOY_138) go test -v -count=1 -timeout 60s ./...

# 1.39-dev, safe-filter only: no crash-filter loaded. Must PASS.
run-139-safe: build $(ENVOY_139) kill-stale
	@echo "=== 1.39-dev safe-only (expect: safe=PASS) ==="
	ENVOY_BIN=$(CURDIR)/$(ENVOY_139) ENVOY_YAML=envoy-safe.yaml \
	  go test -v -count=1 -timeout 60s -run TestSafeFilter ./...

# 1.39-dev, full config: crash-filter triggers the segfault. Expected to fail.
# Exit code 2 = envoy crashed before ready; exit code 1 = test failure.
# run-139-crash always exits 0: annotates the result instead of failing CI.
run-139-crash: build $(ENVOY_139) kill-stale
	@echo "=== 1.39-dev crash (expect: SEGFAULT during load) ==="
	ENVOY_BIN=$(CURDIR)/$(ENVOY_139) \
	  go test -v -count=1 -timeout 60s ./... ; \
	EXIT=$$? ; \
	if [ $$EXIT -ne 0 ]; then \
	  echo "EXPECTED: 1.39-dev crashed (exit $$EXIT): bug confirmed" ; \
	else \
	  echo "UNEXPECTED: 1.39-dev did not crash: bug may be fixed!" ; \
	fi

clean:
	rm -f $(SO) libtestdym.h $(SO_LINUX) libtestdym-linux-amd64.h
	rm -rf .bin/
