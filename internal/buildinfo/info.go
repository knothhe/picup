// Package buildinfo provides offline, allowlisted metadata about this binary.
package buildinfo

import (
	"fmt"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/knothhe/picup"
)

type Dependency struct {
	Name, Module, Version, Purpose, URL, LicenseURL string
	LocalReplacement                                bool
}

type Info struct {
	Version, GoVersion, OS, Arch string
	Revision                     string
	RevisionTime                 time.Time
	Modified, HasRevision        bool
	Dependencies                 []Dependency
}

var majorDependencies = []Dependency{
	{Name: "MyGo", Module: "github.com/egoist/mygo", Purpose: "原生桌面运行时", URL: "https://github.com/egoist/mygo", LicenseURL: "https://github.com/egoist/mygo/blob/main/LICENSE"},
	{Name: "Onevra", Module: "github.com/oneo-me/onevra/source", Purpose: "界面组件与图标", URL: "https://github.com/oneo-me/onevra", LicenseURL: "https://pkg.go.dev/github.com/oneo-me/onevra/source?tab=licenses"},
	{Name: "AWS SDK for Go · S3", Module: "github.com/aws/aws-sdk-go-v2/service/s3", Purpose: "S3 与 R2 对象存储", URL: "https://github.com/aws/aws-sdk-go-v2", LicenseURL: "https://github.com/aws/aws-sdk-go-v2/blob/main/LICENSE.txt"},
	{Name: "Imaging", Module: "github.com/disintegration/imaging", Purpose: "图片缩放与处理", URL: "https://github.com/disintegration/imaging", LicenseURL: "https://github.com/disintegration/imaging/blob/master/LICENSE"},
	{Name: "AVIF", Module: "github.com/gen2brain/avif", Purpose: "AVIF 编解码", URL: "https://github.com/gen2brain/avif", LicenseURL: "https://github.com/gen2brain/avif/blob/main/LICENSE"},
	{Name: "WebP", Module: "github.com/gen2brain/webp", Purpose: "WebP 编解码", URL: "https://github.com/gen2brain/webp", LicenseURL: "https://github.com/gen2brain/webp/blob/main/LICENSE"},
	{Name: "r2connect", Module: "github.com/knothhe/r2connect", Purpose: "R2 授权与连接管理", URL: "https://github.com/knothhe/r2connect", LicenseURL: "https://pkg.go.dev/github.com/knothhe/r2connect?tab=licenses"},
}

var readCurrent = sync.OnceValue(func() Info {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return fromBuildInfo(nil)
	}
	return fromBuildInfo(info)
})

// Current does not read Git, a workspace manifest, user settings or the network.
func Current() Info {
	info := readCurrent()
	info.Dependencies = append([]Dependency(nil), info.Dependencies...)
	return info
}

var revisionPattern = regexp.MustCompile(`^[a-fA-F0-9]{40}([a-fA-F0-9]{24})?$`)
var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+() /-]{0,159}$`)

func cleanVersion(value string) string {
	if !versionPattern.MatchString(value) {
		return ""
	}
	return value
}

func fromBuildInfo(raw *debug.BuildInfo) Info {
	info := Info{Version: picup.Version(), GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, Dependencies: append([]Dependency(nil), majorDependencies...)}
	if raw == nil {
		return info
	}
	if value := cleanVersion(raw.GoVersion); value != "" {
		info.GoVersion = value
	}
	for _, setting := range raw.Settings {
		switch setting.Key {
		case "vcs.revision":
			if revisionPattern.MatchString(setting.Value) {
				info.Revision = strings.ToLower(setting.Value)
				info.HasRevision = true
			}
		case "vcs.time":
			if parsed, e := time.Parse(time.RFC3339, setting.Value); e == nil {
				info.RevisionTime = parsed.UTC()
			}
		case "vcs.modified":
			info.Modified = setting.Value == "true"
		}
	}
	for i := range info.Dependencies {
		d := &info.Dependencies[i]
		for _, module := range raw.Deps {
			if module.Path != d.Module {
				continue
			}
			d.Version = cleanVersion(module.Version)
			if module.Replace != nil {
				// A replacement directory may contain private user paths. Expose
				// only its declared version, never the replacement path.
				d.LocalReplacement = module.Replace.Version == ""
				d.Version = cleanVersion(module.Replace.Version)
			}
			break
		}
	}
	return info
}

// Source describes the runtime packaging mode; it is display information and
// must not be used as an update-signature or installation trust decision.
func Source(packaged, development bool) string {
	if packaged {
		if development {
			return "development-package"
		}
		return "application-package"
	}
	if development {
		return "development-executable"
	}
	return "standalone-executable"
}

// Diagnostics intentionally excludes configuration, environment variables,
// command-line arguments, endpoint/bucket identities and filesystem paths.
func (info Info) Diagnostics(source string) string {
	allowed := map[string]bool{"development-package": true, "application-package": true, "development-executable": true, "standalone-executable": true}
	if !allowed[source] {
		source = "unavailable"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Picup %s\nPlatform: %s/%s\nGo: %s\nSource: %s\n", info.Version, info.OS, info.Arch, info.GoVersion, source)
	if info.HasRevision {
		fmt.Fprintf(&b, "Commit: %s\nModified: %t\n", info.Revision, info.Modified)
	} else {
		b.WriteString("Commit: unavailable\n")
	}
	if !info.RevisionTime.IsZero() {
		fmt.Fprintf(&b, "Commit time: %s\n", info.RevisionTime.Format(time.RFC3339))
	}
	for _, dependency := range info.Dependencies {
		version := dependency.Version
		if dependency.LocalReplacement {
			version = "local replacement"
		} else if version == "" {
			version = "unavailable"
		}
		fmt.Fprintf(&b, "%s: %s\n", dependency.Module, version)
	}
	return b.String()
}
