// Package main is a c-shared .so that registers two HTTP filters against
// the upstream Envoy Go SDK (github.com/envoyproxy/envoy/.../sdk/go).
//
// safe-filter: factory does NOT call back into Envoy during Create.
//              Works on both 1.38.0 and 1.39-dev.
//
// crash-filter: factory calls h.DefineCounter() during Create.
//               Works on 1.38.0; crashes 1.39-dev with:
//               "Caught Segmentation fault: 11, suspect faulting address 0x0"
//               (#0: runtime.sigfwdgo)
package main

import (
	"fmt"
	"os"

	_ "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"

	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// ---- safe-filter --------------------------------------------------------
// No C callback in Create. Baseline: must pass on both versions.

type safeFilter struct{ shared.EmptyHttpFilter }

type safeFactory struct{}

func (safeFactory) Create(_ shared.HttpFilterHandle) shared.HttpFilter {
	return &safeFilter{}
}
func (safeFactory) OnDestroy() {}

type safeConfigFactory struct{}

func (safeConfigFactory) Create(
	_ shared.HttpFilterConfigHandle,
	_ []byte,
) (shared.HttpFilterFactory, error) {
	fmt.Fprintln(os.Stderr, "safe-filter: Create called (no C callback)")
	return safeFactory{}, nil
}

func (safeConfigFactory) CreatePerRoute(_ []byte) (any, error) { return nil, nil }

// ---- crash-filter -------------------------------------------------------
// Calls h.DefineCounter during Create. Triggers crash on 1.39-dev macOS.

type crashFilter struct{ shared.EmptyHttpFilter }

type crashFactory struct{}

func (crashFactory) Create(_ shared.HttpFilterHandle) shared.HttpFilter {
	return &crashFilter{}
}
func (crashFactory) OnDestroy() {}

type crashConfigFactory struct{}

func (crashConfigFactory) Create(
	h shared.HttpFilterConfigHandle,
	_ []byte,
) (shared.HttpFilterFactory, error) {
	fmt.Fprintln(os.Stderr, "crash-filter: about to call DefineCounter (nested CGo)")
	_, _ = h.DefineCounter("test_requests", "status")
	fmt.Fprintln(os.Stderr, "crash-filter: DefineCounter returned OK")
	return crashFactory{}, nil
}

func (crashConfigFactory) CreatePerRoute(_ []byte) (any, error) { return nil, nil }

// ---- registration -------------------------------------------------------

func init() {
	sdk.RegisterHttpFilterConfigFactories(map[string]shared.HttpFilterConfigFactory{
		"safe-filter":  safeConfigFactory{},
		"crash-filter": crashConfigFactory{},
	})
}

func main() {}
