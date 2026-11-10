// verify-update checks a local release DMG and its update feed exactly as an
// installed Picup would, without installing or running the application.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/knothhe/picup"
	"github.com/knothhe/picup/internal/updatedmg"
)

func verify(ctx context.Context, feedPath, archive, public, version, arch string) (updatedmg.BundleReport, error) {
	var report updatedmg.BundleReport
	if version == "" || (arch != "arm64" && arch != "amd64") {
		return report, errors.New("explicit expected version and arm64/amd64 architecture are required")
	}
	info, err := os.Lstat(feedPath)
	if err != nil {
		return report, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return report, errors.New("feed is not a regular file of at most one MiB")
	}
	data, err := os.ReadFile(feedPath)
	if err != nil {
		return report, err
	}
	var m updatedmg.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return report, errors.New("invalid update feed")
	}
	if m.Version != version {
		return report, errors.New("feed version does not match the expected release")
	}
	if err := updatedmg.ValidateManifest(m, updatedmg.Repository, arch); err != nil {
		return report, err
	}
	if filepath.Base(archive) != updatedmg.AssetName(version, arch) {
		return report, errors.New("local DMG name does not match the expected version and architecture")
	}
	if err := updatedmg.Inspect(ctx, archive, m, public, arch, ""); err != nil {
		return report, err
	}
	return updatedmg.BundleReport{Version: version, Architecture: arch, Identifier: updatedmg.BundleIdentifier,
		Stage: "signature-dmg-and-bundle-verified"}, nil
}

func main() {
	flags := flag.NewFlagSet("verify-update", flag.ExitOnError)
	feed := flags.String("feed", "", "local update feed JSON")
	archive := flags.String("archive", "", "local release DMG")
	version := flags.String("version", "", "explicit expected release version")
	arch := flags.String("arch", "", "expected darwin architecture: arm64 or amd64")
	public := flags.String("public-key", picup.UpdateConfig().PublicKey, "trusted Ed25519 public key (defaults to the compiled release key)")
	flags.Parse(os.Args[1:])
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	report, err := verify(ctx, *feed, *archive, *public, *version, *arch)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify-update:", err)
		os.Exit(1)
	}
	json.NewEncoder(os.Stdout).Encode(report)
}
