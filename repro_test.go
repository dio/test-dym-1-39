// Package testdym1_39 is an integration test that demonstrates the Envoy 1.39-dev
// segfault when a Go dynamic module calls back into Envoy (DefineCounter,
// DefineGauge, DefineHistogram) during on_http_filter_config_new on macOS.
//
// Two test cases:
//   TestSafeFilter: no C callback in Create, passes on both versions
//   TestCrashFilter: DefineCounter in Create, passes on 1.38, crashes 1.39-dev macOS
//
// Environment variables:
//   ENVOY_BIN   path to envoy binary (default: .bin/envoy-138/envoy)
//   ENVOY_YAML  which config to load (default: envoy.yaml = both filters)
//               use envoy-safe.yaml to load only safe-filter (no crash risk)
//
// Build libtestdym.so before running: make build
//
// CI usage:
//   make run-138              # both PASS
//   make run-139-safe         # safe=PASS (1.39-dev, safe-only config)
//   make run-139-crash        # crash=SEGFAULT (1.39-dev, expected failure)
package testdym1_39

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

const (
	safeAddr  = "http://localhost:10000"
	crashAddr = "http://localhost:10001"
	adminAddr = "http://localhost:9901"
)

var (
	envoyCmd   *exec.Cmd
	configPath string
)

func TestMain(m *testing.M) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Dir(file)

	bin := envoyBin(root)
	if _, err := os.Stat(bin); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: envoy not found at %s\n", bin)
		os.Exit(0)
	}

	soPath := filepath.Join(root, "libtestdym.so")
	if _, err := os.Stat(soPath); err != nil {
		fmt.Fprintf(os.Stderr, "libtestdym.so not found: run make build\n")
		os.Exit(1)
	}

	cfg := os.Getenv("ENVOY_YAML")
	if cfg == "" {
		cfg = "envoy.yaml"
	}
	configPath = filepath.Join(root, cfg)
	fmt.Fprintf(os.Stderr, "config: %s\n", configPath)

	envoyCmd = exec.Command(bin,
		"-c", configPath,
		"--log-level", "warning",
		"--component-log-level", "dynamic_modules:info",
	)
	envoyCmd.Env = append(os.Environ(),
		"GODEBUG=cgocheck=0",
		"ENVOY_DYNAMIC_MODULES_SEARCH_PATH="+root,
	)
	envoyCmd.Stdout = os.Stderr
	envoyCmd.Stderr = os.Stderr

	if err := envoyCmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "envoy start failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "envoy pid=%d\n", envoyCmd.Process.Pid)

	if !waitReady(20 * time.Second) {
		envoyCmd.Process.Kill()
		fmt.Fprintln(os.Stderr, "envoy not ready in time (crashed during load?)")
		fmt.Fprintln(os.Stderr, "HINT: on 1.39-dev macOS, crash-filter triggers runtime.sigfwdgo at 0x0 via DefineCounter in on_http_filter_config_new")
		os.Exit(2)
	}
	fmt.Fprintln(os.Stderr, "envoy ready")

	code := m.Run()
	envoyCmd.Process.Kill()
	envoyCmd.Wait()
	os.Exit(code)
}

// TestSafeFilter verifies a filter with no C callback in Create.
// Expected: PASS on 1.38.0 and 1.39-dev (both configs).
func TestSafeFilter(t *testing.T) {
	resp, err := http.Get(safeAddr + "/ping")
	if err != nil {
		t.Fatalf("GET safe-filter: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	if string(body) != "safe-ok" {
		t.Fatalf("want 'safe-ok', got %q", body)
	}
}

// TestCrashFilter verifies a filter calling DefineCounter in Create.
// Expected: PASS on 1.38.0, CRASH on 1.39-dev macOS.
// With envoy-safe.yaml the test is skipped (crash-filter not loaded).
// The crash manifests as Envoy dying during load; TestMain exits with
// code 2 before this test is reached.
func TestCrashFilter(t *testing.T) {
	cfg := os.Getenv("ENVOY_YAML")
	if cfg == "envoy-safe.yaml" {
		t.Skip("crash-filter not loaded in envoy-safe.yaml")
	}
	resp, err := http.Get(crashAddr + "/ping")
	if err != nil {
		t.Fatalf("GET crash-filter: %v (on 1.39-dev macOS this follows the segfault from DefineCounter in on_http_filter_config_new)", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, body)
	}
	if string(body) != "crash-ok" {
		t.Fatalf("want 'crash-ok', got %q", body)
	}
}

func envoyBin(root string) string {
	if b := os.Getenv("ENVOY_BIN"); b != "" {
		return b
	}
	return filepath.Join(root, ".bin", "envoy-138", "envoy")
}

func waitReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(adminAddr + "/ready")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
