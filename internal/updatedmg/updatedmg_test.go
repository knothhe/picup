package updatedmg

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

const testVersion = "1.2.3"

func signed(t *testing.T, content []byte) (Manifest, string, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	return Manifest{Version: testVersion, URL: AssetURL(Repository, testVersion, "arm64"), Size: int64(len(content)),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, digest[:]))}, base64.StdEncoding.EncodeToString(public), private
}

func TestValidateManifestBindsExactReleaseAsset(t *testing.T) {
	m, _, _ := signed(t, []byte("dmg"))
	if err := ValidateManifest(m, Repository, "arm64"); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Manifest){
		"other repo":   func(m *Manifest) { m.URL = AssetURL("someone/picup", testVersion, "arm64") },
		"other arch":   func(m *Manifest) { m.URL = AssetURL(Repository, testVersion, "amd64") },
		"tarball":      func(m *Manifest) { m.URL = strings.TrimSuffix(m.URL, ".dmg") + ".tar.gz" },
		"other tag":    func(m *Manifest) { m.URL = strings.Replace(m.URL, "/v1.2.3/", "/v1.2.4/", 1) },
		"zero size":    func(m *Manifest) { m.Size = 0 },
		"huge":         func(m *Manifest) { m.Size = maxDMGSize + 1 },
		"delta":        func(m *Manifest) { m.Deltas = []json.RawMessage{json.RawMessage(`{}`)} },
		"previous":     func(m *Manifest) { m.Previous = json.RawMessage(`{"version":"1.0.0"}`) },
		"bad sig":      func(m *Manifest) { m.Signature = "AAAA" },
		"bad version":  func(m *Manifest) { m.Version = "1.2"; m.URL = AssetURL(Repository, "1.2", "arm64") },
		"path version": func(m *Manifest) { m.Version = "1.2.3/../x"; m.URL = AssetURL(Repository, m.Version, "arm64") },
	} {
		c := m
		mutate(&c)
		if err := ValidateManifest(c, Repository, "arm64"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestVerifySignatureRejectsTamperingWrongKeyAndSize(t *testing.T) {
	content := []byte("signed dmg fixture")
	m, public, _ := signed(t, content)
	if err := VerifySignature(bytes.NewReader(content), m.Size, m.Signature, public); err != nil {
		t.Fatal(err)
	}
	_, other, _ := signed(t, content)
	if VerifySignature(bytes.NewReader(content), m.Size, m.Signature, other) == nil {
		t.Fatal("accepted another key")
	}
	tampered := append([]byte(nil), content...)
	tampered[0] ^= 1
	if VerifySignature(bytes.NewReader(tampered), m.Size, m.Signature, public) == nil {
		t.Fatal("accepted modified bytes")
	}
	if VerifySignature(bytes.NewReader(append(content, 'x')), m.Size, m.Signature, public) == nil {
		t.Fatal("accepted a longer file")
	}
}

var (
	binaryOnce sync.Once
	binaryPath string
	binaryErr  error
)

// nativeBinary builds a tiny native Mach-O executable once.
func nativeBinary(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("macOS bundle fixtures need darwin")
	}
	binaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "picup-updatedmg-bin-")
		if err != nil {
			binaryErr = err
			return
		}
		src := filepath.Join(dir, "main.go")
		if binaryErr = os.WriteFile(src, []byte("package main\nfunc main() {}\n"), 0o600); binaryErr != nil {
			return
		}
		binaryPath = filepath.Join(dir, "fixture")
		cmd := exec.Command("go", "build", "-o", binaryPath, src)
		cmd.Env = append(os.Environ(), "GOOS=darwin", "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0", "GOFLAGS=", "GOWORK=off")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			binaryErr = err
			t.Log(string(out))
		}
	})
	if binaryErr != nil {
		t.Fatal(binaryErr)
	}
	return binaryPath
}

func writeBundle(t *testing.T, parent, version string) string {
	t.Helper()
	bin := nativeBinary(t)
	app := filepath.Join(parent, BundleName)
	for _, dir := range []string{"Contents/MacOS", "Contents/Resources"} {
		if err := os.MkdirAll(filepath.Join(app, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>` + BundleIdentifier + `</string>
<key>CFBundleExecutable</key><string>Picup</string>
<key>CFBundleVersion</key><string>` + version + `</string>
<key>CFBundleShortVersionString</key><string>` + version + `</string>
<key>CFBundlePackageType</key><string>APPL</string>
</dict></plist>
`
	for name, content := range map[string][]byte{
		"Contents/Info.plist":                        []byte(plist),
		"Contents/MacOS/Picup":                       data,
		"Contents/Resources/picup":                   data,
		"Contents/Resources/THIRD_PARTY_NOTICES.txt": []byte("notices\n"),
	} {
		if err := os.WriteFile(filepath.Join(app, name), content, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("/usr/bin/codesign", "--force", "--deep", "--sign", "-", app).CombinedOutput(); err != nil {
		t.Fatalf("codesign: %v %s", err, out)
	}
	return app
}

func TestVerifyBundleChecksIdentityArchitectureAndLinks(t *testing.T) {
	app := writeBundle(t, t.TempDir(), testVersion)
	if err := VerifyBundle(app, testVersion, runtime.GOARCH); err != nil {
		t.Fatal(err)
	}
	if VerifyBundle(app, "9.9.9", runtime.GOARCH) == nil {
		t.Fatal("accepted another version")
	}
	other := map[string]string{"arm64": "amd64", "amd64": "arm64"}[runtime.GOARCH]
	if VerifyBundle(app, testVersion, other) == nil {
		t.Fatal("accepted another architecture")
	}
	inside := filepath.Join(app, "Contents/Resources/inside")
	if err := os.Symlink("picup", inside); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBundle(app, testVersion, runtime.GOARCH); err != nil {
		t.Fatalf("rejected a link inside the bundle: %v", err)
	}
	if err := os.Symlink("../../../outside", filepath.Join(app, "Contents/Resources/escape")); err != nil {
		t.Fatal(err)
	}
	if VerifyBundle(app, testVersion, runtime.GOARCH) == nil {
		t.Fatal("accepted a link that escapes the bundle")
	}
}

// releaseDMG makes a DMG laid out like MyGo's: Picup.app and an Applications link.
func releaseDMG(t *testing.T, version string) (string, Manifest, string) {
	t.Helper()
	if _, err := os.Stat("/usr/bin/hdiutil"); err != nil {
		t.Skip("hdiutil unavailable")
	}
	stage := t.TempDir()
	writeBundle(t, stage, version)
	if err := os.Symlink("/Applications", filepath.Join(stage, "Applications")); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(t.TempDir(), AssetName(version, runtime.GOARCH))
	if out, err := exec.Command("/usr/bin/hdiutil", "create", "-quiet", "-volname", "Picup", "-srcfolder", stage,
		"-format", "UDZO", "-ov", image).CombinedOutput(); err != nil {
		t.Fatalf("hdiutil create: %v %s", err, out)
	}
	content, err := os.ReadFile(image)
	if err != nil {
		t.Fatal(err)
	}
	m, public, _ := signed(t, content)
	m.Version, m.URL = version, AssetURL(Repository, version, runtime.GOARCH)
	return image, m, public
}

func TestInspectStagesOnlyTheVerifiedBundle(t *testing.T) {
	image, m, public := releaseDMG(t, testVersion)
	destination := filepath.Join(t.TempDir(), "app")
	if err := Inspect(context.Background(), image, m, public, runtime.GOARCH, destination); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(destination)
	if err != nil || len(entries) != 1 || entries[0].Name() != BundleName {
		t.Fatalf("staged %v, %v; want only Picup.app", entries, err)
	}
	if err := VerifyBundle(filepath.Join(destination, BundleName), testVersion, runtime.GOARCH); err != nil {
		t.Fatal(err)
	}
	// Another version inside a correctly signed DMG is not this release.
	wrong := m
	wrong.Version = "1.2.4"
	if Inspect(context.Background(), image, wrong, public, runtime.GOARCH, "") == nil {
		t.Fatal("accepted a DMG whose app has another version")
	}
	_, other, _ := signed(t, []byte("x"))
	if Inspect(context.Background(), image, m, other, runtime.GOARCH, "") == nil {
		t.Fatal("accepted a DMG signed with another key")
	}
	if out, _ := exec.Command("/usr/bin/hdiutil", "info").CombinedOutput(); strings.Contains(string(out), "picup-dmg-") {
		t.Fatalf("a verification volume is still mounted:\n%s", out)
	}
}

type rewrite struct{ target *url.URL }

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme, clone.URL.Host = r.target.Scheme, r.target.Host
	clone.URL.Path = "/" + req.URL.Host + req.URL.Path
	clone.Host = r.target.Host
	return http.DefaultTransport.(*http.Transport).Clone().RoundTrip(clone)
}

func TestSourceServesValidatedFeedAndExtracts(t *testing.T) {
	image, m, public := releaseDMG(t, testVersion)
	content, err := os.ReadFile(image)
	if err != nil {
		t.Fatal(err)
	}
	feed, _ := json.Marshal(m)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/github.com/" + Repository + "/releases/latest/download/update-darwin-" + runtime.GOARCH + ".json":
			w.Write(feed)
		case strings.TrimPrefix(m.URL, "https:/"):
			w.Write(content)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	s := NewSource(Repository, public, &http.Client{Transport: rewrite{target}})
	if _, err := s.Open(context.Background(), m.URL); err == nil {
		t.Fatal("opened a URL no manifest named")
	}
	body, err := s.Manifest(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	body.Close()
	download, err := s.Open(context.Background(), m.URL)
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(t.TempDir(), "update.dmg")
	data, _ := io.ReadAll(download)
	download.Close()
	if err := os.WriteFile(local, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Extract(context.Background(), m.URL, local, filepath.Join(t.TempDir(), "app")); err != nil {
		t.Fatal(err)
	}
	if s.Extract(context.Background(), "https://example.com/x.dmg", local, filepath.Join(t.TempDir(), "app")) == nil {
		t.Fatal("extracted for an unknown URL")
	}
	bad := m
	bad.URL = strings.TrimSuffix(bad.URL, ".dmg") + ".tar.gz"
	feed, _ = json.Marshal(bad)
	if _, err := s.Manifest(context.Background(), false); err == nil {
		t.Fatal("accepted a feed that does not point at the release DMG")
	}
}
