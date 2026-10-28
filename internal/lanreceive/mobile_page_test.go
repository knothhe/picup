package lanreceive

import (
	"os/exec"
	"testing"
)

func TestMobilePageSelectionAndRetry(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is not installed")
	}
	if err := exec.Command(node, "-e", `process.exit(typeof require('node:buffer').File === 'function' && typeof FormData === 'function' ? 0 : 1)`).Run(); err != nil {
		t.Skip("Node.js with File and FormData support is not installed")
	}
	if output, err := exec.Command(node, "testdata/mobile_page_test.js").CombinedOutput(); err != nil {
		t.Fatalf("mobile page regression: %v\n%s", err, output)
	}
}
