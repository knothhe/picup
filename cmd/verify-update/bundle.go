package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/knothhe/picup"
	"github.com/knothhe/picup/internal/updatefeed"
)

type bundleReport struct {
	Version      string `json:"version"`
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Identifier   string `json:"identifier"`
	Stage        string `json:"stage"`
}

// verifyBundle checks local copies only; it never installs or executes the app.
func verifyBundle(feedPath, archivePath, public, version, goos, arch string) (bundleReport, error) {
	var report bundleReport
	if version == "" || (goos != "darwin" && goos != "linux") || (arch != "arm64" && arch != "amd64") {
		return report, errors.New("explicit expected version, supported OS and architecture are required")
	}
	info, err := os.Lstat(feedPath)
	if err != nil {
		return report, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return report, errors.New("feed must be a regular file no larger than one MiB")
	}
	content, err := os.ReadFile(feedPath)
	if err != nil {
		return report, err
	}
	feed, err := updatefeed.ParseFeed(content, picup.UpdateConfig().GitHub)
	if err != nil {
		return report, err
	}
	if feed.Version != version {
		return report, errors.New("feed version does not match the expected release")
	}
	target := goos + "-" + arch
	asset, ok := feed.Platforms[target]
	if !ok || filepath.Base(archivePath) != "Picup-"+version+"-"+target+".zip" {
		return report, errors.New("archive does not match the expected release platform")
	}
	if err := updatefeed.VerifyArchive(archivePath, asset, public, version, target); err != nil {
		return report, err
	}
	return bundleReport{version, goos, arch, "com.knothhe.picup", "signature-and-archive-verified"}, nil
}
