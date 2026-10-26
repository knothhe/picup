package r2oauth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/r2connect/keyring"
)

type manualMemoryBackend struct {
	data                  map[string][]byte
	loads, saves, deletes int
	failSave              bool
	denyLoads             int
}

func (b *manualMemoryBackend) Load(service, key string) ([]byte, error) {
	b.loads++
	if b.denyLoads > 0 {
		b.denyLoads--
		return nil, keyring.ErrAccessDenied
	}
	value, ok := b.data[service+"/"+key]
	if !ok {
		return nil, keyring.ErrNotFound
	}
	return append([]byte(nil), value...), nil
}
func (b *manualMemoryBackend) Save(service, key string, value []byte) error {
	b.saves++
	if b.failSave {
		return errors.New("fixture save failure")
	}
	b.data[service+"/"+key] = append([]byte(nil), value...)
	return nil
}
func (b *manualMemoryBackend) Delete(service, key string) error {
	b.deletes++
	delete(b.data, service+"/"+key)
	return nil
}
func manualFixture() (config.Config, *ManualManager, *manualMemoryBackend) {
	current := config.Default()
	current.Endpoint, current.Bucket, current.PublicURL = "https://example.r2.cloudflarestorage.com", "photos", "https://images.example.com"
	current.AccessKey, current.SecretKey, current.SessionToken = "manual-access", "manual-secret", "manual-session"
	current.EnsureProfiles()
	backend := &manualMemoryBackend{data: map[string][]byte{}}
	return current, &ManualManager{store: keyring.NewWithBackend("manual-new", backend), legacy: keyring.NewWithBackend("manual-old", backend)}, backend
}

func TestManualMigrationCommitsReferenceOnlyAfterVerifiedSDKStore(t *testing.T) {
	original, manager, backend := manualFixture()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, original); err != nil {
		t.Fatal(err)
	}
	calls := 0
	migrated, err := manager.Store(t.Context(), original, func(candidate config.Config) error {
		calls++
		if len(backend.data) == 0 || backend.loads == 0 {
			t.Fatal("committed before SDK secret verification")
		}
		id := strings.TrimPrefix(candidate.CredentialRef, manualSDKReferencePrefix)
		saved, err := manager.store.Load("manual-" + id)
		if err != nil {
			t.Fatal(err)
		}
		var record manualRecord
		if json.Unmarshal(saved, &record) != nil || record.AccessKey != original.AccessKey {
			t.Fatal("incomplete SDK credential")
		}
		return config.Save(path, candidate)
	})
	if err != nil || calls != 1 || !strings.HasPrefix(migrated.CredentialRef, manualSDKReferencePrefix) {
		t.Fatal(migrated.CredentialRef, err)
	}
	data, _ := os.ReadFile(path)
	for _, secret := range []string{original.AccessKey, original.SecretKey, original.SessionToken} {
		if strings.Contains(string(data), secret) {
			t.Fatal("plaintext credential persisted")
		}
	}
	reopened, err := config.Load(path)
	if err != nil || reopened.CredentialRef != migrated.CredentialRef {
		t.Fatal("SDK reference did not survive reopening", err)
	}
	credentials, err := manager.Resolve(t.Context(), reopened.CredentialRef, original.Endpoint, original.Bucket)
	if err != nil || credentials.AccessKey != original.AccessKey || credentials.SecretKey != original.SecretKey || credentials.SessionToken != original.SessionToken {
		t.Fatal(credentials, err)
	}
	if original.AccessKey == "" || original.CredentialRef != "" {
		t.Fatal("mutated caller draft")
	}
}

func TestManualSDKFailuresKeepConfigurationAndRecoverableInput(t *testing.T) {
	for _, stage := range []string{"save", "verify", "commit"} {
		t.Run(stage, func(t *testing.T) {
			original, manager, backend := manualFixture()
			path := filepath.Join(t.TempDir(), "config.json")
			if err := config.Save(path, original); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			if stage == "save" {
				backend.failSave = true
			}
			if stage == "verify" {
				backend.denyLoads = 1
			}
			committed := false
			returned, err := manager.Store(t.Context(), original, func(candidate config.Config) error {
				committed = true
				if stage == "commit" {
					return errors.New("fixture config failure")
				}
				return config.Save(path, candidate)
			})
			if err == nil || stage != "commit" && committed {
				t.Fatal("failed store committed config", err)
			}
			after, _ := os.ReadFile(path)
			if string(before) != string(after) || !reflect.DeepEqual(original, returned) {
				t.Fatal("failure lost persisted or recoverable input")
			}
		})
	}
}

func TestManualSDKDenialStopsAutomaticAccessUntilExplicitSave(t *testing.T) {
	original, manager, backend := manualFixture()
	migrated, err := manager.Store(t.Context(), original, func(config.Config) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	backend.denyLoads = 1
	if _, err := manager.Resolve(t.Context(), migrated.CredentialRef, original.Endpoint, original.Bucket); !errors.Is(err, ErrManualDenied) {
		t.Fatal(err)
	}
	calls := backend.loads
	for range 3 {
		if _, err := manager.Resolve(t.Context(), migrated.CredentialRef, original.Endpoint, original.Bucket); !errors.Is(err, ErrManualDenied) {
			t.Fatal(err)
		}
	}
	if backend.loads != calls {
		t.Fatal("background Resolve retried a denied SDK keyring")
	}
	if _, err := manager.Store(t.Context(), original, func(config.Config) error { return nil }); err != nil {
		t.Fatal("explicit save could not retry SDK access", err)
	}
}

func TestManualLegacyReferenceStillReadsThroughSDKKeyring(t *testing.T) {
	original, manager, backend := manualFixture()
	id := strings.Repeat("a", 32)
	record := manualRecord{Endpoint: original.Endpoint, Bucket: original.Bucket, ManualCredentials: ManualCredentials{original.AccessKey, original.SecretKey, original.SessionToken}}
	seedLegacyBlob(t, manager.legacy, id, record)
	saves := backend.saves
	credentials, err := manager.Resolve(t.Context(), id, original.Endpoint, original.Bucket)
	if err != nil || credentials.AccessKey != original.AccessKey {
		t.Fatal(credentials, err)
	}
	if backend.saves != saves {
		t.Fatal("legacy Resolve rewrote credentials or chunks")
	}
	if _, err := manager.Resolve(t.Context(), id, "https://other.example.com", original.Bucket); err == nil {
		t.Fatal("legacy credential escaped destination binding")
	}
}

func TestManualReplacementRetainsSharedSDKGeneration(t *testing.T) {
	original, manager, _ := manualFixture()
	migrated, err := manager.Store(t.Context(), original, func(config.Config) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	replacement := migrated.Clone()
	replacement.AccessKey, replacement.SecretKey = "replacement-access", "replacement-secret"
	updated, err := manager.Store(t.Context(), replacement, func(config.Config) error { return nil })
	if err != nil || updated.CredentialRef == migrated.CredentialRef {
		t.Fatal(updated.CredentialRef, err)
	}
	previous, err := manager.Resolve(t.Context(), migrated.CredentialRef, original.Endpoint, original.Bucket)
	if err != nil || previous.AccessKey != original.AccessKey {
		t.Fatal("replacement removed shared old SDK generation", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := manager.Resolve(canceled, migrated.CredentialRef, original.Endpoint, original.Bucket); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestManualNamespacesFollowCanonicalDataDirectoryAndChannel(t *testing.T) {
	directory := t.TempDir()
	if NewManual(filepath.Join(directory, "config.json")) != NewManual(filepath.Join(directory, ".", "config.json")) {
		t.Fatal("canonical paths do not share coordinator")
	}
	if NewManual(filepath.Join(directory, "config.json")) == NewManual(filepath.Join(t.TempDir(), "config.json")) {
		t.Fatal("independent paths share coordinator")
	}
	if namespaceForChannel(filepath.Join(directory, "config.json"), "development") == namespaceForChannel(filepath.Join(directory, "config.json"), "release") {
		t.Fatal("channels share keyring namespace")
	}
	if newSDKSecrets(filepath.Join(directory, "config.json")) == newSDKSecrets(filepath.Join(directory, ".", "config.json")) {
		t.Fatal("SDK factory shared a callback-bearing store before Manager ownership")
	}
}
