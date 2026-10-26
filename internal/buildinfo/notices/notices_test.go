package notices

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureFile(t *testing.T, root, name, text string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRenderIncludesNestedNativeLicensesAndNoticesDeterministically(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "go/LICENSE", "Go license\n")
	fixtureFile(t, root, "first/LICENSE", "First module license\n")
	fixtureFile(t, root, "first/lib/LICENSE.libavif", "Native libavif license\n")
	fixtureFile(t, root, "first/lib/PATENTS.aom", "Native patent grant\n")
	fixtureFile(t, root, "second/LICENSE.txt", "Second module license\n")
	fixtureFile(t, root, "second/NOTICE.txt", "Required attribution\n")
	modules := []Module{{Path: "example.test/b", Version: "v2.0.0", Dir: filepath.Join(root, "second")}, {Path: "example.test/a", Version: "v1.0.0", Dir: filepath.Join(root, "first")}}
	first, err := Render(modules, filepath.Join(root, "go"))
	if err != nil {
		t.Fatal(err)
	}
	modules[0], modules[1] = modules[1], modules[0]
	second, err := Render(modules, filepath.Join(root, "go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("notice output depends on discovery order")
	}
	for _, expected := range []string{"example.test/a v1.0.0", "example.test/b v2.0.0", "Native libavif license", "Native patent grant", "Required attribution", "Go license"} {
		if !bytes.Contains(first, []byte(expected)) {
			t.Errorf("missing %q", expected)
		}
	}
	if strings.Contains(string(first), root) {
		t.Fatal("absolute source directory leaked into artifact")
	}
}

func TestRenderRejectsMissingLicenseInsteadOfSilentlyOmittingDependency(t *testing.T) {
	root := t.TempDir()
	fixtureFile(t, root, "go/LICENSE", "Go license")
	if err := os.MkdirAll(filepath.Join(root, "module"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Render([]Module{{Path: "example.test/missing", Dir: filepath.Join(root, "module")}}, filepath.Join(root, "go")); err == nil {
		t.Fatal("missing attribution text silently omitted")
	}
}
