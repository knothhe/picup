package r2oauth

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/knothhe/picup/internal/config"
)

func TestCredentialNamespaceSeparatesChannelsForExplicitSharedConfig(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared", "config.json")
	t.Setenv("PICUP_CONFIG", shared)
	path, err := config.Path()
	if err != nil || path != shared {
		t.Fatal("explicit configuration path changed", err)
	}
	release := namespaceForChannel(path, "release")
	development := namespaceForChannel(path, "development")
	if release == development {
		t.Fatal("development and release credentials share a namespace")
	}
	if !strings.HasPrefix(release, "com.knothhe.picup.r2oauth.release.") || !strings.HasPrefix(development, "com.knothhe.picup.r2oauth.development.") {
		t.Fatal("namespace does not identify its channel")
	}
	if Channel != config.BuildChannel || namespace(path) != namespaceForChannel(path, config.BuildChannel) {
		t.Fatal("credential namespace diverges from the compiled configuration identity")
	}
}

func TestCredentialNamespaceAlsoSeparatesDataDirectories(t *testing.T) {
	root := t.TempDir()
	first := namespace(filepath.Join(root, "one", "config.json"))
	second := namespace(filepath.Join(root, "two", "config.json"))
	if first == second {
		t.Fatal("distinct data directories share credentials")
	}
}
