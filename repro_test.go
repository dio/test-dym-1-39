// Package testdym1_39 is an integration test that demonstrates the 1.39-dev
// nested CGo segfault during on_http_filter_config_new on macOS.
//
// Two test cases:
//   TestSafeFilter  -- no C callback in Create -- passes on both versions
//   TestCrashFilter -- DefineCounter in Create -- passes on 1.38, crashes 1.39-dev
//
// Prerequisites:
//   go test -v -run TestSafeFilter   ENVOY_BIN=<path>
//   make run-138   # runs both with 1.38.0 (expect: both PASS)
//   make run-139   # runs both with 1.39-dev (expect: safe PASS, crash FAIL/segfault)
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
	envoyCmd  *exec.Cmd
	soDir     string
)

func TestMain(m *testing.M) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Dir(file)
	soDir = root

	bin := envoyBin()
	if _, err := os.Stat(bin); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: envoy not found at %s\n", bin)
		os.Exit(0)
	}

	soPath := filepath.Join(root, "libtestdym.so")
	if os.Getenv("SKIP_BUILD") == "" {
		fmt.Fprintln(os.Stderr, "building libtestdym.so ...")
		cmd := exec.Command("go", "build", "-trimpath", "-buildmode=c-shared",
			"-o", soPath, "./filter")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "build failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "build OK")
	}

	envoyCmd = exec.Command(bin,
		"-c", filepath.Join(root, "envoy.yaml"),
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

	if !waitReady(15 * time.Second) {
		envoyCmd.Process.Kill()
		fmt.Fprintln(os.Stderr, "envoy not ready in time (crashed during load?)")
		// Report the failure here so the test output is clear.
		fmt.Fprintln(os.Stderr, "HINT: on 1.39-dev this is the expected crash from DefineCounter in on_http_filter_config_new")
		os.Exit(2)
	}
	fmt.Fprintln(os.Stderr, "envoy ready")

	code := m.Run()
	envoyCmd.Process.Kill()
	envoyCmd.Wait()
	os.Exit(code)
}

// TestSafeFilter verifies that a filter with no C callback in Create works.
// Expected: PASS on both 1.38.0 and 1.39-dev.
func TestSafeFilter(t *testing.T) {
	resp, err := http.Get(safeAddr + "/ping")
	if err != nil {
		t.Fatalf("GET /ping: %v", err)
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

// TestCrashFilter verifies that a filter calling DefineCounter in Create works.
// Expected: PASS on 1.38.0, CRASH/FAIL on 1.39-dev macOS.
// The crash manifests as Envoy dying during load (TestMain exits with code 2
// before this test runs) or as a connection error here.
func TestCrashFilter(t *testing.T) {
	resp, err := http.Get(crashAddr + "/ping")
	if err != nil {
		t.Fatalf("GET /ping (crash filter port): %v -- on 1.39-dev this is the segfault from DefineCounter in on_http_filter_config_new", err)
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

func envoyBin() string {
	if b := os.Getenv("ENVOY_BIN"); b != "" {
		return b
	}
	// default to 1.38.0 via func-e
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".func-e", "versions", "1.38.0", "bin", "envoy")
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
