package picup

import (
	"encoding/json"
	"testing"
)

func TestPublicUpdaterConfigUsesEmbeddedManifest(t *testing.T) {
	var expected struct {
		Updates UpdateSettings `json:"updates"`
		Version string         `json:"version"`
	}
	if err := json.Unmarshal(manifest, &expected); err != nil {
		t.Fatal(err)
	}
	if got := UpdateConfig(); got != expected.Updates {
		t.Fatalf("updater config diverged from package manifest: %#v", got)
	}
	if Version() != expected.Version {
		t.Fatal("version diverged from package manifest")
	}
}
