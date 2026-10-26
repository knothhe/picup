package gui

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestSecondInstanceKeepsFirstFileAndResolvesLaunchingDirectory(t *testing.T) {
	working := t.TempDir()
	absolute := filepath.Join(t.TempDir(), "other.png")
	if got, want := secondInstanceFiles([]string{"first.png", absolute, ""}, working), []string{filepath.Join(working, "first.png"), absolute}; !reflect.DeepEqual(got, want) {
		t.Fatal(got, want)
	}
	if len(secondInstanceFiles(nil, working)) != 0 {
		t.Fatal("empty launch created an upload")
	}
}
