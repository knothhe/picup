package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/knothhe/picup/internal/updatedmg"
)

func TestVerifyRejectsFeedsThatDoNotNameTheExpectedDMG(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, updatedmg.AssetName("1.2.3", "arm64"))
	if err := os.WriteFile(archive, []byte("dmg"), 0o600); err != nil {
		t.Fatal(err)
	}
	signature := strings.Repeat("A", 86) + "=="
	url := updatedmg.AssetURL(updatedmg.Repository, "1.2.3", "arm64")
	for name, tc := range map[string]struct{ feed, version, arch, archive string }{
		"no version":    {`{}`, "", "arm64", archive},
		"bad arch":      {`{}`, "1.2.3", "386", archive},
		"other version": {`{"version":"1.2.4","url":"` + url + `","size":3,"signature":"` + signature + `"}`, "1.2.3", "arm64", archive},
		"tarball":       {`{"version":"1.2.3","url":"` + strings.TrimSuffix(url, ".dmg") + `.tar.gz","size":3,"signature":"` + signature + `"}`, "1.2.3", "arm64", archive},
		"renamed":       {`{"version":"1.2.3","url":"` + url + `","size":3,"signature":"` + signature + `"}`, "1.2.3", "arm64", filepath.Join(dir, "Picup.dmg")},
	} {
		feed := filepath.Join(dir, "update-darwin-arm64.json")
		if err := os.WriteFile(feed, []byte(tc.feed), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := verify(context.Background(), feed, tc.archive, "", tc.version, tc.arch); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
