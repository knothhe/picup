package updatefeed

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Capture this before replacing files: Linux may subsequently report a deleted
// executable path through /proc/self/exe.
var startExecutable, _ = os.Executable()

type Config struct {
	Repository     string
	PublicKey      string
	CurrentVersion string
	OS             string
	Arch           string
	Executable     string
}

type Client struct {
	config Config
	http   *http.Client
	mu     sync.Mutex
	// Replaced only by isolated package tests.
	rename         func(string, string) error
	verifyPlatform func(context.Context, string) error
}

func New(config Config) *Client {
	if config.OS == "" {
		config.OS = runtime.GOOS
	}
	if config.Arch == "" {
		config.Arch = runtime.GOARCH
	}
	if config.Executable == "" {
		config.Executable = startExecutable
	}
	return &Client{config: config, http: &http.Client{
		Timeout: 10 * time.Minute,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if request.URL.Scheme != "https" || len(via) >= 10 {
				return errors.New("update redirect is insecure or excessive")
			}
			return nil
		},
	}, rename: os.Rename, verifyPlatform: verifyPlatformBundle}
}

func (c *Client) target() (string, error) {
	config := c.config
	if !supportedTarget(config.OS+"-"+config.Arch) || !validVersion(config.CurrentVersion) || !repositoryPattern.MatchString(config.Repository) {
		return "", errors.New("updates are not configured for this build")
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(config.PublicKey))
	if err != nil || len(key) != 32 {
		return "", errors.New("invalid configured update public key")
	}
	executable, err := filepath.EvalSymlinks(config.Executable)
	if err != nil || !filepath.IsAbs(executable) {
		return "", errors.New("cannot resolve installed executable")
	}
	if strings.Contains(filepath.ToSlash(executable), "/Cellar/") || strings.Contains(filepath.ToSlash(executable), "/Caskroom/") || os.Getenv("APPIMAGE") != "" {
		return "", errors.New("this installation is managed by another installer")
	}
	target := filepath.Dir(executable)
	if config.OS == "darwin" {
		target = filepath.Dir(filepath.Dir(target))
		if !strings.HasSuffix(target, ".app") || filepath.Base(executable) != "Picup" || filepath.Base(filepath.Dir(executable)) != "MacOS" || filepath.Base(filepath.Dir(filepath.Dir(executable))) != "Contents" {
			return "", errors.New("updates require an installed Picup application bundle")
		}
		content, err := os.ReadFile(filepath.Join(target, "Contents", "Info.plist"))
		if err != nil {
			return "", err
		}
		if err := validatePlist(content, config.CurrentVersion); err != nil {
			return "", err
		}
	} else {
		if filepath.Base(executable) != "picup-gui" {
			return "", errors.New("updates require the dedicated Picup application directory")
		}
		if err := validateLinuxInstall(target, config.CurrentVersion, config.Arch); err != nil {
			return "", err
		}
	}
	info, err := os.Lstat(target)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("application target is not a regular directory")
	}
	return target, nil
}

func validateLinuxInstall(target, version, arch string) error {
	allowed := map[string]bool{"picup-gui": true, "picup": true, "icon.png": true, "THIRD_PARTY_NOTICES.txt": true, "picup-release.json": true}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != len(allowed) {
		return errors.New("application directory contains missing or unmanaged files")
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !allowed[entry.Name()] || !info.Mode().IsRegular() || info.Size() <= 0 {
			return errors.New("application directory contains an unmanaged file or link")
		}
	}
	content, err := os.ReadFile(filepath.Join(target, "picup-release.json"))
	var got ReleaseMetadata
	if err != nil || len(content) > 64<<10 || json.Unmarshal(content, &got) != nil || got != metadata(version, "linux", arch) {
		return errors.New("installed Linux application identity does not match")
	}
	return nil
}

func (c *Client) Enabled() bool {
	target, err := c.target()
	if err != nil {
		return false
	}
	probe, err := os.MkdirTemp(filepath.Dir(target), ".picup-update-probe-")
	if err != nil {
		return false
	}
	return os.Remove(probe) == nil
}

type Update struct {
	Version string
	Notes   string
	Date    time.Time
	client  *Client
	asset   Asset
}

func (c *Client) get(ctx context.Context, address string) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil || request.URL.Scheme != "https" {
		return nil, errors.New("updates require HTTPS")
	}
	request.Header.Set("User-Agent", "Picup/"+c.config.CurrentVersion)
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("update download: %s", response.Status)
	}
	return response.Body, nil
}

func (c *Client) Check(ctx context.Context) (*Update, error) {
	if !c.Enabled() {
		return nil, errors.New("updates are unavailable for this installation")
	}
	body, err := c.get(ctx, "https://github.com/"+c.config.Repository+"/releases/latest/download/"+Filename)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	content, err := io.ReadAll(io.LimitReader(body, MaxFeedSize+1))
	if err != nil {
		return nil, err
	}
	feed, err := ParseFeed(content, c.config.Repository)
	if err != nil {
		return nil, err
	}
	asset, ok := feed.Platforms[c.config.OS+"-"+c.config.Arch]
	if !ok {
		return nil, errors.New("this release has no update for the current platform")
	}
	if CompareVersions(feed.Version, c.config.CurrentVersion) <= 0 {
		return nil, nil
	}
	date, _ := time.Parse(time.RFC3339, feed.Date)
	return &Update{Version: feed.Version, Notes: feed.Notes, Date: date, client: c, asset: asset}, nil
}

// Install verifies final bytes in a private sibling staging directory, then
// replaces only the dedicated application directory. A failed second rename
// restores the original directory; user settings are stored elsewhere.
func (u *Update) Install(ctx context.Context, progress func(downloaded, total int64)) error {
	c := u.client
	c.mu.Lock()
	defer c.mu.Unlock()
	target, err := c.target()
	if err != nil {
		return err
	}
	if CompareVersions(u.Version, c.config.CurrentVersion) <= 0 {
		return errors.New("refusing a non-newer update")
	}
	work, err := os.MkdirTemp(filepath.Dir(target), ".picup-update-")
	if err != nil {
		return err
	}
	defer func() {
		if work != "" {
			os.RemoveAll(work)
		}
	}()
	archive := filepath.Join(work, "update.zip")
	if err := u.download(ctx, archive, progress); err != nil {
		return err
	}
	platform := c.config.OS + "-" + c.config.Arch
	if err := VerifyArchive(archive, u.asset, c.config.PublicKey, u.Version, platform); err != nil {
		return err
	}
	stage := filepath.Join(work, "new")
	if err := extractZIP(ctx, archive, stage, u.Version, platform); err != nil {
		return err
	}
	application := filepath.Join(stage, archiveRoot(platform))
	if c.config.OS == "darwin" {
		if err := c.verifyPlatform(ctx, application); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Recheck before replacing: downloaded bytes do not authorize deleting a
	// directory whose ownership/content changed while the request was running.
	if checked, err := c.target(); err != nil || checked != target {
		return errors.New("application target changed during update preparation")
	}
	old := filepath.Join(work, "old")
	if err := c.rename(target, old); err != nil {
		return err
	}
	if err := c.rename(application, target); err != nil {
		if restore := c.rename(old, target); restore != nil {
			// Preserve the recovery directory when restoring fails.
			recovery := work + "-recovery"
			if move := os.Rename(work, recovery); move == nil {
				return fmt.Errorf("install failed: %v; restore failed: %v; original application retained at %s", err, restore, filepath.Join(recovery, "old"))
			}
			// Avoid the deferred cleanup removing the only remaining app.
			work = ""
			return fmt.Errorf("install failed: %v; restore failed: %v; original application retained at %s", err, restore, old)
		}
		return err
	}
	return nil
}

func (u *Update) download(ctx context.Context, filename string, progress func(int64, int64)) error {
	body, err := u.client.get(ctx, u.asset.URL)
	if err != nil {
		return err
	}
	defer body.Close()
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	var count int64
	buffer := make([]byte, 64<<10)
	stream := io.LimitReader(body, u.asset.Size+1)
	for err == nil {
		if err = ctx.Err(); err != nil {
			break
		}
		var n int
		n, err = stream.Read(buffer)
		if n > 0 {
			count += int64(n)
			if count > u.asset.Size {
				err = errors.New("downloaded ZIP exceeds declared size")
				break
			}
			if _, writeErr := file.Write(buffer[:n]); writeErr != nil {
				err = writeErr
				break
			}
			if progress != nil {
				progress(count, u.asset.Size)
			}
		}
	}
	closeErr := file.Close()
	if err != nil && err != io.EOF {
		return err
	}
	if count != u.asset.Size {
		return errors.New("downloaded ZIP size does not match feed")
	}
	return closeErr
}

func extractZIP(ctx context.Context, filename, destination, version, target string) error {
	archive, err := zip.OpenReader(filename)
	if err != nil {
		return err
	}
	defer archive.Close()
	entries, err := inspectArchive(&archive.Reader, version, target)
	if err != nil {
		return err
	}
	var names []string
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	// Files and directories first. Links cannot redirect any extraction write.
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry := entries[name]
		if entry.Mode()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(destination, filepath.FromSlash(name))
		if entry.Mode().IsDir() {
			if err := os.MkdirAll(path, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		stream, err := entry.Open()
		if err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, entry.Mode().Perm()&0755)
		if err != nil {
			stream.Close()
			return err
		}
		n, err := io.Copy(file, io.LimitReader(stream, int64(entry.UncompressedSize64)+1))
		stream.Close()
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if n != int64(entry.UncompressedSize64) {
			return errors.New("extracted ZIP file size differs")
		}
	}
	for _, name := range names {
		entry := entries[name]
		if entry.Mode()&os.ModeSymlink == 0 {
			continue
		}
		stream, err := entry.Open()
		if err != nil {
			return err
		}
		link, err := io.ReadAll(io.LimitReader(stream, 4097))
		stream.Close()
		if err != nil {
			return err
		}
		path := filepath.Join(destination, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.Symlink(string(link), path); err != nil {
			return err
		}
	}
	return nil
}

func verifyPlatformBundle(ctx context.Context, application string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("macOS signature verification requires macOS")
	}
	if output, err := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", application).CombinedOutput(); err != nil {
		return fmt.Errorf("updated application code signature is invalid: %s", strings.TrimSpace(string(output)))
	}
	return nil
}
