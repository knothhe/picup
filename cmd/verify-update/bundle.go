package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"debug/macho"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type updateFeed struct {
	Version   string            `json:"version"`
	URL       string            `json:"url"`
	Size      int64             `json:"size"`
	Signature string            `json:"signature"`
	Deltas    []json.RawMessage `json:"deltas"`
	Previous  json.RawMessage   `json:"previous"`
}
type bundleReport struct {
	Version      string `json:"version"`
	Architecture string `json:"architecture"`
	Identifier   string `json:"identifier"`
	Stage        string `json:"stage"`
}

func plistStrings(content []byte) (map[string]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "dict" {
			break
		}
	}
	values := map[string]string{}
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if end, ok := token.(xml.EndElement); ok && end.Name.Local == "dict" {
			return values, nil
		}
		keyStart, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if keyStart.Name.Local != "key" {
			return nil, errors.New("invalid top-level property list dictionary")
		}
		var key string
		if err := decoder.DecodeElement(&key, &keyStart); err != nil {
			return nil, err
		}
		for {
			token, err = decoder.Token()
			if err != nil {
				return nil, err
			}
			if _, ok := token.(xml.StartElement); ok {
				break
			}
		}
		start := token.(xml.StartElement)
		if start.Name.Local == "string" {
			var value string
			if err := decoder.DecodeElement(&value, &start); err != nil {
				return nil, err
			}
			if _, exists := values[key]; exists {
				return nil, errors.New("duplicate property list key")
			}
			values[key] = value
		} else if err := decoder.Skip(); err != nil {
			return nil, err
		}
	}
}

// verifyBundle checks local copies only. It neither extracts nor executes the
// application and cannot certify a live install, startup or rollback.
func verifyBundle(feedPath, archivePath, public, version, arch string) (bundleReport, error) {
	var report bundleReport
	if version == "" || (arch != "arm64" && arch != "amd64") {
		return report, errors.New("explicit expected version and arm64/amd64 architecture are required")
	}
	feedInfo, err := os.Stat(feedPath)
	if err != nil {
		return report, err
	}
	if feedInfo.Size() > 1<<20 {
		return report, errors.New("feed exceeds one MiB")
	}
	feedBytes, err := os.ReadFile(feedPath)
	if err != nil {
		return report, err
	}
	var feed updateFeed
	if json.Unmarshal(feedBytes, &feed) != nil {
		return report, errors.New("invalid update feed")
	}
	if feed.Version != version {
		return report, errors.New("feed version does not match the expected release")
	}
	if len(feed.Deltas) != 0 || (len(feed.Previous) != 0 && string(feed.Previous) != "null") {
		return report, errors.New("verification expects a full update archive without delta metadata")
	}
	expectedArchive := fmt.Sprintf("picup-%s-darwin-%s.tar.gz", version, arch)
	if feed.URL != "https://github.com/knothhe/picup/releases/download/v"+version+"/"+expectedArchive {
		return report, errors.New("feed URL does not identify the exact expected release archive")
	}
	if filepath.Base(archivePath) != expectedArchive {
		return report, errors.New("local archive name does not match the expected version and architecture")
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		return report, err
	}
	if info.Size() != feed.Size || feed.Size <= 0 {
		return report, errors.New("archive size does not match the feed")
	}
	if err := verify(archivePath, public, feed.Signature); err != nil {
		return report, err
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return report, err
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return report, err
	}
	defer compressed.Close()
	archive := tar.NewReader(compressed)
	seen := map[string]bool{}
	required := map[string]bool{"Picup.app/Contents/Info.plist": false, "Picup.app/Contents/MacOS/Picup": false, "Picup.app/Contents/Resources/picup": false, "Picup.app/Contents/Resources/THIRD_PARTY_NOTICES.txt": false}
	var total int64
	for count := 0; ; count++ {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return report, err
		}
		name := strings.TrimSuffix(header.Name, "/")
		if count >= 10000 || path.IsAbs(name) || path.Clean(name) != name || !(name == "Picup.app" || strings.HasPrefix(name, "Picup.app/")) || seen[name] {
			return report, errors.New("invalid, duplicate or excessive update archive paths")
		}
		seen[name] = true
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeSymlink {
			return report, errors.New("archive contains an unsupported special file")
		}
		total += header.Size
		if header.Size < 0 || total > 2<<30 {
			return report, errors.New("uncompressed update archive exceeds its size limit")
		}
		if header.Typeflag == tar.TypeSymlink {
			target := path.Join(path.Dir(name), header.Linkname)
			if path.IsAbs(header.Linkname) || !strings.HasPrefix(path.Clean(target), "Picup.app/") {
				return report, errors.New("archive link escapes the application bundle")
			}
		}
		if _, needed := required[name]; !needed {
			continue
		}
		if !header.FileInfo().Mode().IsRegular() || header.Size > 256<<20 {
			return report, errors.New("required application file is linked, missing or too large")
		}
		content, err := io.ReadAll(io.LimitReader(archive, 256<<20+1))
		if err != nil {
			return report, err
		}
		switch name {
		case "Picup.app/Contents/Info.plist":
			values, err := plistStrings(content)
			if err != nil {
				return report, err
			}
			if values["CFBundleIdentifier"] != "com.knothhe.picup" || values["CFBundleExecutable"] != "Picup" || values["CFBundleVersion"] != version || values["CFBundleShortVersionString"] != version {
				return report, errors.New("archive application identity or version does not match")
			}
		case "Picup.app/Contents/MacOS/Picup", "Picup.app/Contents/Resources/picup":
			binary, err := macho.NewFile(bytes.NewReader(content))
			if err != nil {
				return report, fmt.Errorf("%s is not a Mach-O executable", name)
			}
			expected := macho.CpuArm64
			if arch == "amd64" {
				expected = macho.CpuAmd64
			}
			if binary.Cpu != expected || binary.Type != macho.TypeExec || header.Mode&0111 == 0 {
				return report, errors.New("archive executable architecture or mode does not match")
			}
			binary.Close()
		case "Picup.app/Contents/Resources/THIRD_PARTY_NOTICES.txt":
			if len(content) == 0 {
				return report, errors.New("empty third-party license notices")
			}
		}
		required[name] = true
	}
	for name, present := range required {
		if !present {
			return report, fmt.Errorf("archive missing required file: %s", name)
		}
	}
	return bundleReport{version, arch, "com.knothhe.picup", "signature-and-archive-verified"}, nil
}
