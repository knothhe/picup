package r2oauth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/r2connect"
)

const manualSDKReferencePrefix = "keyring:"

type ManualCredentials struct {
	AccessKey    string `json:"access_key"`
	SecretKey    string `json:"secret_key"`
	SessionToken string `json:"session_token,omitempty"`
}
type manualRecord struct {
	Endpoint string `json:"endpoint"`
	Bucket   string `json:"bucket"`
	ManualCredentials
}
type ManualManager struct {
	mu     sync.Mutex
	store  r2connect.SecretStore
	legacy r2connect.SecretStore
}

var manualManagers sync.Map
var ErrManualDenied = errors.New("system credential access was denied; allow access and actively save the manual credentials again to retry")

func NewManual(path string) *ManualManager {
	path = canonicalConfigPath(path)
	created := &ManualManager{store: systemKeyring(namespace(path)+".manual.sdk", "Picup manual S3 credentials"), legacy: systemKeyring(namespace(path)+".manual", "Picup legacy manual S3 credentials")}
	actual, _ := manualManagers.LoadOrStore(path, created)
	return actual.(*ManualManager)
}
func ManualStorageAvailable() bool { return systemStoreAvailable() }
func manualError(err error) error {
	if errors.Is(err, ErrSecretDenied) {
		return ErrManualDenied
	}
	if errors.Is(err, errSecretMissing) {
		return errors.New("saved manual credentials were not found; re-enter the access keys in storage settings")
	}
	return err
}
func (m *ManualManager) Resolve(ctx context.Context, ref, endpoint, bucket string) (ManualCredentials, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return ManualCredentials{}, err
	}
	var record manualRecord
	if id, ok := strings.CutPrefix(ref, manualSDKReferencePrefix); ok {
		if !legacyIDPattern.MatchString(id) {
			return ManualCredentials{}, errors.New("invalid saved manual credential reference")
		}
		data, err := m.store.Load("manual-" + id)
		if err != nil {
			return ManualCredentials{}, manualError(err)
		}
		err = json.Unmarshal(data, &record)
		clear(data)
		if err != nil {
			return ManualCredentials{}, errors.New("saved manual credentials are invalid")
		}
	} else {
		if err := readLegacyBlob(ctx, m.legacy, ref, &record); err != nil {
			return ManualCredentials{}, manualError(err)
		}
	}
	if record.Endpoint != endpoint || record.Bucket != bucket {
		return ManualCredentials{}, errors.New("saved manual credentials belong to a different storage destination; save replacement credentials for this destination")
	}
	if record.AccessKey == "" || record.SecretKey == "" {
		return ManualCredentials{}, errors.New("saved manual credentials are incomplete; re-enter the access keys in storage settings")
	}
	return record.ManualCredentials, ctx.Err()
}

// Store is explicit: SDK writes and verification precede configuration commit.
// Old generations remain available to copied profiles.
func (m *ManualManager) Store(ctx context.Context, current config.Config, commit func(config.Config) error) (config.Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return current, err
	}
	if current.R2Connection != "" {
		return current, errors.New("Cloudflare OAuth already manages its credentials")
	}
	if current.AccessKey == "" || current.SecretKey == "" {
		return current, errors.New("access key and secret key are required")
	}
	if commit == nil {
		return current, errors.New("configuration commit is required")
	}
	for _, store := range []r2connect.SecretStore{m.store, m.legacy} {
		if retry, ok := store.(interface{ RetryAccess() }); ok {
			retry.RetryAccess()
		}
	}
	record := manualRecord{Endpoint: current.Endpoint, Bucket: current.Bucket, ManualCredentials: ManualCredentials{current.AccessKey, current.SecretKey, current.SessionToken}}
	encoded, err := json.Marshal(record)
	if err != nil {
		return current, err
	}
	defer clear(encoded)
	id, err := randomID()
	if err != nil {
		return current, err
	}
	key := "manual-" + id
	if err := m.store.Save(key, encoded); err != nil {
		return current, manualError(err)
	}
	rollback := func() { _ = m.store.Delete(key) }
	verified, err := m.store.Load(key)
	if err != nil {
		rollback()
		return current, manualError(err)
	}
	var decoded manualRecord
	err = json.Unmarshal(verified, &decoded)
	clear(verified)
	if err != nil || decoded != record {
		rollback()
		return current, errors.New("stored manual credentials did not pass verification; previous credentials were retained")
	}
	candidate := current.Clone()
	candidate.CredentialRef = manualSDKReferencePrefix + id
	candidate.AccessKey, candidate.SecretKey, candidate.SessionToken = "", "", ""
	candidate.EnsureProfiles()
	if err := candidate.ValidateOptions(); err != nil {
		rollback()
		return current, err
	}
	if err := ctx.Err(); err != nil {
		rollback()
		return current, err
	}
	if err := commit(candidate); err != nil {
		rollback()
		return current, err
	}
	return candidate, nil
}
