package laptopsetup

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBootstrapForwardsSignalsWaitsForChildAndCleansUp(t *testing.T) {
	tests := []struct {
		name       string
		signal     syscall.Signal
		wantMarker string
		wantExit   int
	}{
		{name: "interrupt", signal: syscall.SIGINT, wantMarker: "INT", wantExit: 130},
		{name: "terminate", signal: syscall.SIGTERM, wantMarker: "TERM", wantExit: 143},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rootDir := t.TempDir()
			fakeBin := filepath.Join(rootDir, "bin")
			bootstrapTmp := filepath.Join(rootDir, "tmp")
			if err := os.MkdirAll(fakeBin, 0o755); err != nil {
				t.Fatalf("create fake bin: %v", err)
			}
			if err := os.MkdirAll(bootstrapTmp, 0o755); err != nil {
				t.Fatalf("create bootstrap temp root: %v", err)
			}

			writeExecutable(t, filepath.Join(fakeBin, "uname"), `#!/bin/sh
case "$1" in
  -s) printf 'Darwin\n' ;;
  -m) printf 'arm64\n' ;;
  *) exit 1 ;;
esac
`)
			writeExecutable(t, filepath.Join(fakeBin, "curl"), `#!/bin/sh
output=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    shift
    output=$1
  fi
  shift
done
case "$output" in
  *release.json)
    printf '%s\n' '{"assets": [{"name": "laptop-setup-darwin-arm64", "digest": "sha256:test-digest"}]}' > "$output"
    ;;
  *)
    /bin/cp "$FAKE_BINARY_SOURCE" "$output"
    ;;
esac
`)
			writeExecutable(t, filepath.Join(fakeBin, "shasum"), `#!/bin/sh
printf 'test-digest  %s\n' "$3"
`)

			signalPath := filepath.Join(rootDir, "signal")
			childPIDPath := filepath.Join(rootDir, "child.pid")
			testBinary, err := os.Executable()
			if err != nil {
				t.Fatalf("resolve test binary: %v", err)
			}
			fakeBinary := filepath.Join(rootDir, "fake-laptop-setup")
			writeExecutable(t, fakeBinary, `#!/bin/sh
exec "$TEST_BINARY" -test.run '^TestBootstrapSignalHelperProcess$'
`)

			stdinPath := filepath.Join(rootDir, "stdin")
			cmd := exec.Command("/bin/sh", "bootstrap.sh", "--resume")
			cmd.Dir = "."
			cmd.Stdin = strings.NewReader("preserved bootstrap stdin")
			cmd.Env = append(os.Environ(),
				"PATH="+fakeBin+":/usr/bin:/bin",
				"TMPDIR="+bootstrapTmp,
				"FAKE_BINARY_SOURCE="+fakeBinary,
				"TEST_BINARY="+testBinary,
				"BOOTSTRAP_SIGNAL_HELPER=1",
				"SIGNAL_FILE="+signalPath,
				"CHILD_PID_FILE="+childPIDPath,
				"STDIN_FILE="+stdinPath,
			)
			if err := cmd.Start(); err != nil {
				t.Fatalf("start bootstrap: %v", err)
			}

			childPID := waitForBootstrapPID(t, childPIDPath, 3*time.Second)
			stdinPayload, err := os.ReadFile(stdinPath)
			if err != nil {
				t.Fatalf("read preserved child stdin: %v", err)
			}
			if string(stdinPayload) != "preserved bootstrap stdin" {
				t.Fatalf("downloaded child stdin mismatch: got=%q", stdinPayload)
			}
			if err := cmd.Process.Signal(test.signal); err != nil {
				t.Fatalf("signal bootstrap: %v", err)
			}
			err = waitForCommand(t, cmd, 3*time.Second)
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("expected signaled bootstrap to exit nonzero, got %v", err)
			}
			if exitErr.ExitCode() != test.wantExit {
				t.Fatalf("exit code mismatch: got=%d want=%d", exitErr.ExitCode(), test.wantExit)
			}

			marker, err := os.ReadFile(signalPath)
			if err != nil {
				t.Fatalf("read forwarded signal marker: %v", err)
			}
			if string(marker) != test.wantMarker {
				t.Fatalf("forwarded signal mismatch: got=%q want=%q", marker, test.wantMarker)
			}
			if err := syscall.Kill(childPID, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatalf("downloaded child %d still exists after bootstrap exit: %v", childPID, err)
			}

			entries, err := os.ReadDir(bootstrapTmp)
			if err != nil {
				t.Fatalf("read bootstrap temp root: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("bootstrap temporary directory was not cleaned up: %v", entries)
			}
		})
	}
}

func TestBootstrapSignalHelperProcess(t *testing.T) {
	if os.Getenv("BOOTSTRAP_SIGNAL_HELPER") != "1" {
		return
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	stdinPayload, err := io.ReadAll(os.Stdin)
	if err != nil {
		t.Fatalf("read helper stdin: %v", err)
	}
	if err := os.WriteFile(os.Getenv("STDIN_FILE"), stdinPayload, 0o600); err != nil {
		t.Fatalf("write helper stdin marker: %v", err)
	}
	if err := os.WriteFile(os.Getenv("CHILD_PID_FILE"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatalf("write helper pid: %v", err)
	}
	received := <-signals

	marker := "TERM"
	exitCode := 143
	if received == os.Interrupt {
		marker = "INT"
		exitCode = 130
	}
	if err := os.WriteFile(os.Getenv("SIGNAL_FILE"), []byte(marker), 0o600); err != nil {
		t.Fatalf("write helper signal marker: %v", err)
	}
	os.Exit(exitCode)
}

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %q: %v", path, err)
	}
}

func waitForBootstrapPID(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		payload, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(payload)))
			if parseErr != nil {
				t.Fatalf("parse child pid: %v", parseErr)
			}
			return pid
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read child pid: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for bootstrap child pid at %q", path)
	return 0
}

func waitForCommand(t *testing.T, cmd *exec.Cmd, timeout time.Duration) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		t.Fatal("timed out waiting for command")
		return nil
	}
}
