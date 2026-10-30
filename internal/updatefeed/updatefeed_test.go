package updatefeed

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type zipEntry struct {
	name    string
	mode    os.FileMode
	content []byte
}

func executableFixture(goos, arch string) []byte {
	if goos == "darwin" {
		content := make([]byte, 32)
		binary.LittleEndian.PutUint32(content, 0xfeedfacf)
		cpu := uint32(0x0100000c)
		if arch == "amd64" {
			cpu = 0x01000007
		}
		binary.LittleEndian.PutUint32(content[4:], cpu)
		binary.LittleEndian.PutUint32(content[12:], 2)
		return content
	}
	content := make([]byte, 64)
	copy(content, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(content[16:], 2)
	cpu := uint16(62)
	if arch == "arm64" {
		cpu = 183
	}
	binary.LittleEndian.PutUint16(content[18:], cpu)
	binary.LittleEndian.PutUint32(content[20:], 1)
	binary.LittleEndian.PutUint16(content[52:], 64)
	return content
}

func fixtureEntries(version, target string) []zipEntry {
	goos, arch, _ := strings.Cut(target, "-")
	root := archiveRoot(target)
	executable := executableFixture(goos, arch)
	entries := []zipEntry{{root + "/", os.ModeDir | 0755, nil}}
	if goos == "linux" {
		content, _ := json.Marshal(metadata(version, goos, arch))
		return append(entries, zipEntry{root + "/picup-gui", 0755, executable}, zipEntry{root + "/picup", 0755, executable},
			zipEntry{root + "/picup-release.json", 0644, content}, zipEntry{root + "/icon.png", 0644, []byte("PNG fixture")},
			zipEntry{root + "/THIRD_PARTY_NOTICES.txt", 0644, []byte("Licenses")})
	}
	plist := []byte(`<?xml version="1.0"?><plist><dict><key>CFBundleIdentifier</key><string>com.knothhe.picup</string><key>CFBundleVersion</key><string>` + version + `</string><key>Nested</key><dict><key>CFBundleVersion</key><string>ignored</string></dict><key>CFBundleShortVersionString</key><string>` + version + `</string><key>CFBundleExecutable</key><string>Picup</string></dict></plist>`)
	return append(entries, zipEntry{root + "/Contents/Info.plist", 0644, plist}, zipEntry{root + "/Contents/MacOS/Picup", 0755, executable},
		zipEntry{root + "/Contents/Resources/picup", 0755, executable}, zipEntry{root + "/Contents/Resources/THIRD_PARTY_NOTICES.txt", 0644, []byte("Licenses")})
}

func writeFixtureZIP(t *testing.T, entries []zipEntry) (string, Asset, string) {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "package.zip")
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetMode(entry.mode)
		stream, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stream.Write(entry.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(filename)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	digest := sha256.Sum256(content)
	return filename, Asset{URL: "https://github.com/example/picup/releases/download/v2.0.0/Picup-2.0.0-linux-amd64.zip", Size: int64(len(content)), SHA256: hex.EncodeToString(digest[:]), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, digest[:]))}, base64.StdEncoding.EncodeToString(public)
}

func TestFeedRequiresExactTaggedZIPContract(t *testing.T) {
	feed := Feed{Schema: 1, Version: "2.0.0", Date: "2026-10-29T00:00:00Z", Platforms: map[string]Asset{
		"linux-amd64": {URL: "https://github.com/example/picup/releases/download/v2.0.0/Picup-2.0.0-linux-amd64.zip", Size: 100, SHA256: strings.Repeat("a", 64), Signature: base64.StdEncoding.EncodeToString(make([]byte, 64))},
	}}
	encode := func(value Feed) []byte { content, _ := json.Marshal(value); return content }
	if _, err := ParseFeed(encode(feed), "example/picup"); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Feed){
		func(f *Feed) { f.Schema = 2 },
		func(f *Feed) { f.Version = "02.0.0" },
		func(f *Feed) { f.Version = "2.0.0-01" },
		func(f *Feed) { f.Date = "yesterday" },
		func(f *Feed) { f.Platforms = nil },
		func(f *Feed) {
			a := f.Platforms["linux-amd64"]
			a.URL = strings.ReplaceAll(a.URL, "example/picup", "attacker/picup")
			f.Platforms["linux-amd64"] = a
		},
		func(f *Feed) {
			a := f.Platforms["linux-amd64"]
			a.URL = strings.ReplaceAll(a.URL, ".zip", ".tar.gz")
			f.Platforms["linux-amd64"] = a
		},
		func(f *Feed) {
			a := f.Platforms["linux-amd64"]
			a.Size = MaxArchiveSize + 1
			f.Platforms["linux-amd64"] = a
		},
		func(f *Feed) {
			a := f.Platforms["linux-amd64"]
			a.Signature = "invalid"
			f.Platforms["linux-amd64"] = a
		},
		func(f *Feed) { a := f.Platforms["linux-amd64"]; a.SHA256 = "invalid"; f.Platforms["linux-amd64"] = a },
	} {
		var changed Feed
		json.Unmarshal(encode(feed), &changed)
		change(&changed)
		if _, err := ParseFeed(encode(changed), "example/picup"); err == nil {
			t.Fatalf("invalid feed accepted: %s", encode(changed))
		}
	}
	for _, content := range [][]byte{append(encode(feed), []byte(" {}")...), bytes.Replace(encode(feed), []byte(`"schema":1`), []byte(`"unknown":1,"schema":1`), 1), bytes.Replace(encode(feed), []byte(`"schema":1`), []byte(`"schema":2,"schema":1`), 1), bytes.Repeat([]byte(" "), MaxFeedSize+1)} {
		if _, err := ParseFeed(content, "example/picup"); err == nil {
			t.Fatal("invalid or oversized JSON accepted")
		}
	}
}

func TestVersionComparisonPreservesSemverOrderWithoutOverflow(t *testing.T) {
	ordered := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.10.0", "999999999999999999999999999999.0.0"}
	for i, version := range ordered {
		if !validVersion(version) || CompareVersions(version, version+"+build") != 0 {
			t.Fatal("invalid equality", version)
		}
		for j := 0; j < i; j++ {
			if CompareVersions(ordered[j], version) >= 0 || CompareVersions(version, ordered[j]) <= 0 {
				t.Fatalf("wrong order %s < %s", ordered[j], version)
			}
		}
	}
}

func TestSignedZIPValidatesBothPlatformsAndArchitecture(t *testing.T) {
	for _, target := range []string{"darwin-arm64", "darwin-amd64", "linux-amd64", "linux-arm64"} {
		t.Run(target, func(t *testing.T) {
			filename, asset, public := writeFixtureZIP(t, fixtureEntries("2.0.0", target))
			if err := VerifyArchive(filename, asset, public, "2.0.0", target); err != nil {
				t.Fatal(err)
			}
			if err := VerifyArchive(filename, asset, public, "3.0.0", target); err == nil {
				t.Fatal("wrong package version accepted")
			}
			other := strings.TrimSuffix(strings.TrimSuffix(target, "amd64"), "arm64") + "arm64"
			if other == target {
				other = strings.TrimSuffix(target, "arm64") + "amd64"
			}
			if err := VerifyArchive(filename, asset, public, "2.0.0", other); err == nil {
				t.Fatal("wrong package architecture accepted")
			}
			changed := asset
			changed.SHA256 = strings.Repeat("0", 64)
			if err := VerifyArchive(filename, changed, public, "2.0.0", target); err == nil {
				t.Fatal("wrong package hash accepted")
			}
			changed = asset
			changed.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))
			if err := VerifyArchive(filename, changed, public, "2.0.0", target); err == nil {
				t.Fatal("wrong package signature accepted")
			}
			changed = asset
			changed.Size--
			if err := VerifyArchive(filename, changed, public, "2.0.0", target); err == nil {
				t.Fatal("wrong package size accepted")
			}
		})
	}
}

func TestZIPRejectsEscapingAliasedAndLinkedExtractionPaths(t *testing.T) {
	for _, extra := range []zipEntry{
		{"Picup/../outside", 0644, []byte("outside")},
		{"/Picup/outside", 0644, nil},
		{"Picup\\outside", 0644, nil},
		{"Picup/picup", 0755, []byte("duplicate")},
		{"Picup/PICUP", 0755, []byte("case alias")},
		{"Picup/link", os.ModeSymlink | 0777, []byte("picup")},
		{"Picup/device", os.ModeDevice | 0600, nil},
		{"Picup/unknown-file", 0644, []byte("unmanaged")},
	} {
		t.Run(extra.name, func(t *testing.T) {
			filename, asset, public := writeFixtureZIP(t, append(fixtureEntries("2.0.0", "linux-amd64"), extra))
			if err := VerifyArchive(filename, asset, public, "2.0.0", "linux-amd64"); err == nil {
				t.Fatal("unsafe ZIP accepted")
			}
		})
	}
	for _, extras := range [][]zipEntry{
		{{"Picup.app/Contents/link", os.ModeSymlink | 0777, []byte("../../../outside")}},
		{{"Picup.app/Contents/a", os.ModeSymlink | 0777, []byte("b")}, {"Picup.app/Contents/b", os.ModeSymlink | 0777, []byte("a")}},
		{{"Picup.app/Contents/link", os.ModeSymlink | 0777, []byte("Resources")}, {"Picup.app/Contents/link/injected", 0644, []byte("nested under link")}},
		// Lexically cleaning a/.. before resolving a would wrongly accept
		// this link: a points to the application root, so .. escapes it.
		{{"Picup.app/Contents/a", os.ModeSymlink | 0777, []byte("..")}, {"Picup.app/Contents/b", os.ModeSymlink | 0777, []byte("a/../outside")}, {"Picup.app/Contents/outside", 0644, []byte("decoy")}},
	} {
		filename, asset, public := writeFixtureZIP(t, append(fixtureEntries("2.0.0", "darwin-arm64"), extras...))
		if err := VerifyArchive(filename, asset, public, "2.0.0", "darwin-arm64"); err == nil {
			t.Fatal("unsafe framework link accepted")
		}
	}
}

func TestFrameworkLinksStayLinksAndExecutablesKeepModes(t *testing.T) {
	entries := append(fixtureEntries("2.0.0", "darwin-arm64"),
		zipEntry{"Picup.app/Contents/Frameworks/Example.framework/Versions/A/Example", 0755, []byte("library")},
		zipEntry{"Picup.app/Contents/Frameworks/Example.framework/Versions/Current", os.ModeSymlink | 0777, []byte("A")},
		zipEntry{"Picup.app/Contents/Frameworks/Example.framework/Example", os.ModeSymlink | 0777, []byte("Versions/Current/Example")})
	filename, asset, public := writeFixtureZIP(t, entries)
	if err := VerifyArchive(filename, asset, public, "2.0.0", "darwin-arm64"); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "stage")
	if err := extractZIP(context.Background(), filename, destination, "2.0.0", "darwin-arm64"); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(destination, "Picup.app/Contents/Frameworks/Example.framework/Example")
	if value, err := os.Readlink(link); err != nil || value != "Versions/Current/Example" {
		t.Fatal("framework symbolic link changed", value, err)
	}
	if content, err := os.ReadFile(link); err != nil || string(content) != "library" {
		t.Fatal("framework link did not resolve", err)
	}
	info, err := os.Stat(filepath.Join(destination, "Picup.app/Contents/MacOS/Picup"))
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatal("executable permissions lost", err)
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func clientFixture(t *testing.T) (*Client, string, Asset, string) {
	t.Helper()
	filename, asset, public := writeFixtureZIP(t, fixtureEntries("2.0.0", "linux-amd64"))
	current, _, _ := writeFixtureZIP(t, fixtureEntries("1.0.0", "linux-amd64"))
	parent := t.TempDir()
	if err := extractZIP(context.Background(), current, parent, "1.0.0", "linux-amd64"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "Picup")
	client := New(Config{Repository: "example/picup", PublicKey: public, CurrentVersion: "1.0.0", OS: "linux", Arch: "amd64", Executable: filepath.Join(target, "picup-gui")})
	client.http.Transport = transportFunc(func(request *http.Request) (*http.Response, error) {
		if err := request.Context().Err(); err != nil {
			return nil, err
		}
		var content []byte
		switch request.URL.String() {
		case asset.URL:
			content, _ = os.ReadFile(filename)
		case "https://github.com/example/picup/releases/latest/download/update-feed.json":
			content, _ = json.Marshal(Feed{Schema: 1, Version: "2.0.0", Date: "2026-10-29T00:00:00Z", Notes: "Release notes", Platforms: map[string]Asset{"linux-amd64": asset}})
		default:
			t.Fatalf("unexpected URL: %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(content)), Header: make(http.Header), Request: request}, nil
	})
	return client, target, asset, filename
}

func TestClientUsesOneFeedAndReplacesOnlyDedicatedAppDirectory(t *testing.T) {
	client, target, _, _ := clientFixture(t)
	if !client.Enabled() {
		t.Fatal("valid dedicated Linux installation disabled")
	}
	settings := filepath.Join(filepath.Dir(target), "user-settings.json")
	os.WriteFile(settings, []byte("keep user settings"), 0600)
	update, err := client.Check(context.Background())
	if err != nil || update == nil || update.Version != "2.0.0" || update.Notes != "Release notes" {
		t.Fatal("shared feed did not select platform", update, err)
	}
	var downloaded, total int64
	if err := update.Install(context.Background(), func(n, size int64) { downloaded, total = n, size }); err != nil {
		t.Fatal(err)
	}
	if downloaded == 0 || downloaded != total {
		t.Fatal("missing byte progress")
	}
	if err := validateLinuxInstall(target, "2.0.0", "amd64"); err != nil {
		t.Fatal("new app was not installed", err)
	}
	if content, err := os.ReadFile(settings); err != nil || string(content) != "keep user settings" {
		t.Fatal("update modified external settings", err)
	}
}

func TestClientRefusesUnmanagedDirectoriesAndUnsupportedPlatforms(t *testing.T) {
	client, target, _, _ := clientFixture(t)
	os.WriteFile(filepath.Join(target, "personal.txt"), []byte("keep"), 0644)
	if client.Enabled() {
		t.Fatal("unmanaged directory enabled for replacement")
	}
	os.Remove(filepath.Join(target, "personal.txt"))
	for _, config := range []Config{
		{OS: "windows", Arch: "amd64"},
		{OS: "linux", Arch: "amd64", Executable: "/usr/local/bin/picup"},
		{OS: "linux", Arch: "amd64", Executable: filepath.Join(target, "picup-gui"), PublicKey: "invalid"},
	} {
		if New(config).Enabled() {
			t.Fatal("unsupported or unsigned installation enabled")
		}
	}
}

func TestFailedPreparationAndFailedRenamePreserveOriginalApp(t *testing.T) {
	for _, failure := range []string{"signature", "cancel", "rename", "restore"} {
		t.Run(failure, func(t *testing.T) {
			client, target, _, _ := clientFixture(t)
			update, err := client.Check(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			switch failure {
			case "signature":
				update.asset.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "rename", "restore":
				client.rename = func(source, destination string) error {
					if strings.Contains(source, string(filepath.Separator)+"new"+string(filepath.Separator)) || failure == "restore" && filepath.Base(source) == "old" {
						return errors.New("simulated rename failure")
					}
					return os.Rename(source, destination)
				}
			}
			if err := update.Install(ctx, nil); err == nil {
				t.Fatal("failed operation reported success")
			}
			if failure == "restore" {
				matches, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".picup-update-*-recovery", "old"))
				if len(matches) != 1 || validateLinuxInstall(matches[0], "1.0.0", "amd64") != nil {
					t.Fatal("failed rollback did not preserve recovery application", matches)
				}
			} else if err := validateLinuxInstall(target, "1.0.0", "amd64"); err != nil {
				t.Fatal("failed operation damaged original app", err)
			}
		})
	}
}
