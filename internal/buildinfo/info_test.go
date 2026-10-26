package buildinfo

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestBuildInfoUsesBinaryMetadataAndOmitsPrivateReplacementPaths(t *testing.T) {
	info := fromBuildInfo(&debug.BuildInfo{GoVersion: "go1.27.1", Deps: []*debug.Module{
		{Path: "github.com/egoist/mygo", Version: "v0.5.5"},
		{Path: "github.com/gen2brain/avif", Version: "v0.6.0", Replace: &debug.Module{Path: "/Users/private/internal-fork"}},
	}, Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: strings.Repeat("a", 40)},
		{Key: "vcs.modified", Value: "true"},
		{Key: "vcs.time", Value: "2026-10-26T01:02:03Z"},
		{Key: "-ldflags", Value: "-X internal.PrivateKey=not-for-diagnostics"},
	}})
	if !info.HasRevision || !info.Modified || info.RevisionTime.IsZero() {
		t.Fatal("valid VCS metadata lost")
	}
	if info.Dependencies[0].Version != "v0.5.5" {
		t.Fatal("did not use actual module version")
	}
	if !info.Dependencies[4].LocalReplacement || info.Dependencies[4].Version != "" {
		t.Fatal("local replacement not identified")
	}
	diagnostics := info.Diagnostics("development-executable")
	for _, secret := range []string{"/Users/private", "internal-fork", "PrivateKey", "not-for-diagnostics"} {
		if strings.Contains(diagnostics, secret) {
			t.Fatalf("private value %q leaked", secret)
		}
	}
	if !strings.Contains(diagnostics, "local replacement") || !strings.Contains(diagnostics, "v0.5.5") {
		t.Fatal("diagnostics missing useful safe metadata")
	}
}

func TestBuildInfoMissingAndMalformedMetadataRemainsExplicit(t *testing.T) {
	info := fromBuildInfo(&debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "secret/path"}, {Key: "vcs.time", Value: "yesterday"}}})
	if info.HasRevision || !info.RevisionTime.IsZero() {
		t.Fatal("invalid metadata accepted")
	}
	if !strings.Contains(info.Diagnostics("injected\nsecret"), "Source: unavailable\n") {
		t.Fatal("unrecognized source accepted")
	}
	if !strings.Contains(info.Diagnostics("application-package"), "Commit: unavailable") {
		t.Fatal("missing metadata was guessed")
	}
}

func TestCurrentDoesNotExposeMutableSharedDependencySlice(t *testing.T) {
	first := Current()
	first.Dependencies[0].Name = "changed"
	if Current().Dependencies[0].Name != "MyGo" {
		t.Fatal("callers can mutate cached metadata")
	}
}
