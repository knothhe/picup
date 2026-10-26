package devcontrol

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestDevelopmentControlChildProcess(t *testing.T) {
	if os.Getenv("PICUP_TEST_CONTROL_CHILD") != "1" {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		os.Exit(2)
	}
	server, err := start(executable, func(fn func()) { fn() }, func() { os.Exit(0) })
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer server.Close()
	fmt.Println("control-ready")
	select {}
}

func TestNormalQuitCoordinatesExactProcessAndSurvivesStaleSocket(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS exact process inspection")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	executable = filepath.Join(t.TempDir(), "control-child")
	if err := os.WriteFile(executable, content, 0755); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		child := exec.Command(executable, "-test.run=^TestDevelopmentControlChildProcess$")
		child.Env = append(os.Environ(), "PICUP_TEST_CONTROL_CHILD=1")
		output, err := child.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		// Only test subprocesses are terminated on a test failure.
		t.Cleanup(func() { child.Process.Kill(); child.Wait() })
		ready := make(chan bool, 1)
		go func() {
			scanner := bufio.NewScanner(output)
			ready <- scanner.Scan() && scanner.Text() == "control-ready"
		}()
		select {
		case ok := <-ready:
			if !ok {
				t.Fatal("child failed before readiness")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("child readiness timeout")
		}
		if err := Stop(executable, 5*time.Second); err != nil {
			t.Fatal(err)
		}
		if err := child.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	path, _ := socketPath(executable)
	os.Remove(path)
}
