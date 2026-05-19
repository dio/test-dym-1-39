ENVOY_138     ?= $(HOME)/.func-e/versions/1.38.0/bin/envoy
ENVOY_139_DIR  = .bin/envoy-dev
ENVOY_139      = $(ENVOY_139_DIR)/envoy

# archive-envoy nightly dev release.
# For macOS->Linux cross-build use: ARCHIVE_ENVOY_OS=linux ARCHIVE_ENVOY_ARCH=amd64
ARCHIVE_ENVOY_OS   ?= darwin
ARCHIVE_ENVOY_ARCH ?= arm64
ARCHIVE_ENVOY_TAG  ?= dev

SO         = libtestdym.so
# scripts/zigcc.sh from luwes for cross-compile (linux target)
LUWES_ROOT ?= $(HOME)/src/dio/luwes
ZIGCC      = $(LUWES_ROOT)/scripts/zigcc.sh

.PHONY: all build build-linux clean run-138 run-139 $(ENVOY_139)

all: run-138 run-139

build: $(SO)

$(SO): filter/main.go go.mod
	CGO_ENABLED=1 go build -trimpath -buildmode=c-shared -o $(SO) ./filter
	@# Envoy resolves name="testdym" -> libtestdym.so (lib prefix, .so suffix).

# Cross-compile for linux/amd64 using zig cc toolchain (macOS host).
# Produces libtestdym-linux-amd64.so -- run it against a Linux envoy binary.
build-linux: filter/main.go go.mod
	TARGET=x86_64-linux-gnu CGO_ENABLED=1 CC=$(ZIGCC) GOOS=linux GOARCH=amd64 \
	  go build -trimpath -buildmode=c-shared -o libtestdym-linux-amd64.so ./filter

# Download 1.39-dev from tetratelabs/archive-envoy if not present.
$(ENVOY_139):
	@echo "downloading envoy 1.39-dev ($(ARCHIVE_ENVOY_OS)/$(ARCHIVE_ENVOY_ARCH)) ..."
	@mkdir -p $(ENVOY_139_DIR)
	@curl -fsSL \
	  "https://github.com/tetratelabs/archive-envoy/releases/download/$(ARCHIVE_ENVOY_TAG)/envoy-$(ARCHIVE_ENVOY_TAG)-$(ARCHIVE_ENVOY_OS)-$(ARCHIVE_ENVOY_ARCH).tar.xz" \
	  | tar -xJ -C $(ENVOY_139_DIR) --strip-components=2 \
	      "envoy-$(ARCHIVE_ENVOY_TAG)-$(ARCHIVE_ENVOY_OS)-$(ARCHIVE_ENVOY_ARCH)/bin/envoy"
	@chmod +x $(ENVOY_139)
	@$(ENVOY_139) --version

# Kill stale Envoy processes holding our test ports.
.PHONY: kill-stale
kill-stale:
	@pkill -f "envoy.*envoy.yaml" 2>/dev/null || true
	@sleep 0.3

# 1.38.0: both safe-filter and crash-filter must PASS.
run-138: build kill-stale
	@echo "=== 1.38.0 (expect: safe=PASS crash=PASS) ==="
	ENVOY_BIN=$(ENVOY_138) go test -v -count=1 -timeout 30s ./... ; \
	EXIT=$$? ; \
	pkill -f "envoy.*envoy.yaml" 2>/dev/null || true ; \
	exit $$EXIT

# 1.39-dev: safe-filter PASSES, crash-filter triggers the segfault.
run-139: build $(ENVOY_139) kill-stale
	@echo "=== 1.39-dev (expect: safe=PASS crash=SEGFAULT) ==="
	ENVOY_BIN=$(ENVOY_139) go test -v -count=1 -timeout 30s ./... ; \
	EXIT=$$? ; \
	pkill -f "envoy.*envoy.yaml" 2>/dev/null || true ; \
	exit $$EXIT

clean:
	rm -f $(SO) libtestdym.h libtestdym-linux-amd64.so libtestdym-linux-amd64.h
	rm -rf $(ENVOY_139_DIR)
