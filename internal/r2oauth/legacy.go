package r2oauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/r2connect"
	"github.com/knothhe/r2connect/keyring"
)

var legacyIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var legacyHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var ErrLegacyCleanupRequired = errors.New("此旧版 Cloudflare 连接不能安全自动迁移；请先用旧版断开此连接后再迁移，原资料和系统凭据已保留")
var ErrLegacyLocalOnly = errors.New("旧版 Cloudflare 连接已完成远端清理，请主动断开以完成本地清理")

func LegacyImportBlocked(err error) bool { return errors.Is(err, ErrLegacyCleanupRequired) }

type legacyTokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	Scope        string    `json:"scope"`
}
type legacySecretsRecord struct {
	OAuth     legacyTokens `json:"oauth"`
	AccessKey string       `json:"access_key"`
	SecretKey string       `json:"secret_key"`
}
type legacyRetired struct {
	TokenID  string `json:"token_id,omitempty"`
	SecretID string `json:"secret_id,omitempty"`
}
type legacyRecord struct {
	Connection
	ClientID          string          `json:"client_id"`
	RedirectURL       string          `json:"redirect_url"`
	SecretID          string          `json:"secret_id"`
	TokenID           string          `json:"token_id"`
	TokenDeleted      bool            `json:"token_deleted,omitempty"`
	OAuthRevoked      bool            `json:"oauth_revoked,omitempty"`
	KeepAuthorization bool            `json:"keep_authorization,omitempty"`
	Retired           []legacyRetired `json:"retired,omitempty"`
}
type legacyMetadata struct {
	Version     int                     `json:"version"`
	Connections map[string]legacyRecord `json:"connections"`
}
type legacyManifest struct {
	Parts  int
	SHA256 string
}

const legacyPartBytes = 2000
const legacyMaxBlobBytes = 256 * 1024

type legacyImportEntry struct {
	SourceSHA256 string `json:"source_sha256"`
	StateSHA256  string `json:"state_sha256,omitempty"`
	Imported     bool   `json:"imported,omitempty"`
	Completed    bool   `json:"completed,omitempty"`
	CleanupOnly  bool   `json:"cleanup_only,omitempty"`
}
type legacyJournal struct {
	Version int                          `json:"version"`
	Entries map[string]legacyImportEntry `json:"entries"`
}

func legacySessionDirectory(path string) string { return canonicalConfigPath(path) + ".r2connect" }
func legacyJournalPath(path string) string {
	return filepath.Join(legacySessionDirectory(path), "legacy-import.json")
}
func legacyStatePath(path, id string) string {
	return filepath.Join(legacySessionDirectory(path), id+".json")
}

func readLegacyJSON(path string, value any) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return false, errors.New("旧版 Cloudflare 迁移资料格式无效，原文件已保留")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if json.Unmarshal(data, value) != nil {
		return false, errors.New("旧版 Cloudflare 迁移资料无法读取，原文件已保留")
	}
	return true, nil
}
func loadLegacy(path string) (legacyMetadata, error) {
	metadata := legacyMetadata{Version: 1, Connections: map[string]legacyRecord{}}
	ok, err := readLegacyJSON(canonicalConfigPath(path)+".r2-oauth.json", &metadata)
	if err != nil {
		return metadata, err
	}
	if ok && (metadata.Version != 1 || metadata.Connections == nil) {
		return metadata, errors.New("旧版 Cloudflare 资料版本不受支持，原文件已保留")
	}
	for id, record := range metadata.Connections {
		if !legacyIDPattern.MatchString(id) || record.ID != id {
			return metadata, ErrLegacyCleanupRequired
		}
	}
	return metadata, nil
}
func loadLegacyJournal(path string) (legacyJournal, error) {
	journal := legacyJournal{Version: 1, Entries: map[string]legacyImportEntry{}}
	if info, err := os.Lstat(legacySessionDirectory(path)); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return journal, errors.New("Cloudflare SDK 状态目录不安全，原资料已保留")
		}
	} else if !os.IsNotExist(err) {
		return journal, err
	}
	ok, err := readLegacyJSON(legacyJournalPath(path), &journal)
	if err != nil {
		return journal, err
	}
	if ok && (journal.Version != 1 || journal.Entries == nil) {
		return journal, errors.New("Cloudflare 迁移记录无法读取，原文件已保留")
	}
	for id, entry := range journal.Entries {
		if !legacyIDPattern.MatchString(id) || !legacyHashPattern.MatchString(entry.SourceSHA256) || entry.StateSHA256 != "" && !legacyHashPattern.MatchString(entry.StateSHA256) {
			return journal, ErrLegacyCleanupRequired
		}
	}
	return journal, nil
}
func saveLegacyJournal(path string, journal legacyJournal) error {
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	return writeLegacyJournal(legacyJournalPath(path), append(data, '\n'))
}

var writeLegacyJournal = func(path string, data []byte) error { return config.AtomicWrite(path, data, 0600) }

func legacyDigest(value any) string {
	data, _ := json.Marshal(value)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
func ensureLegacyDirectory(path string) error {
	directory := legacySessionDirectory(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		return errors.New("Cloudflare SDK 状态目录不安全，原资料已保留")
	}
	return nil
}

// ListLegacy only reads public files. It never opens the system credential store.
func ListLegacy(path string) ([]Connection, error) {
	metadata, err := loadLegacy(path)
	if err != nil {
		return nil, err
	}
	journal, err := loadLegacyJournal(path)
	if err != nil {
		return nil, err
	}
	var connections []Connection
	for id, record := range metadata.Connections {
		if entry, ok := journal.Entries[id]; ok {
			if entry.SourceSHA256 != legacyDigest(record) {
				return nil, ErrLegacyCleanupRequired
			}
			if entry.Completed {
				continue
			}
			if entry.Imported {
				if _, exists, err := (r2connect.FileStateStore{Path: legacyStatePath(path, id)}).Load(); err != nil {
					return nil, err
				} else if exists {
					continue
				}
			}
		}
		connection := record.Connection
		connection.PendingCleanup = connection.PendingCleanup || record.TokenDeleted || record.OAuthRevoked || record.KeepAuthorization
		connections = append(connections, connection)
	}
	sort.Slice(connections, func(i, j int) bool { return connections[i].ID < connections[j].ID })
	return connections, nil
}

// LegacyConnection supplies old public labels and cleanup-only state to an SDK
// session without reading credentials, including after import has been marked.
func LegacyConnection(path, id string) (Connection, bool, error) {
	metadata, err := loadLegacy(path)
	if err != nil {
		return Connection{}, false, err
	}
	record, ok := metadata.Connections[id]
	if !ok {
		return Connection{}, false, nil
	}
	journal, err := loadLegacyJournal(path)
	if err != nil {
		return Connection{}, false, err
	}
	entry := journal.Entries[id]
	if entry.SourceSHA256 != "" && entry.SourceSHA256 != legacyDigest(record) {
		return Connection{}, false, ErrLegacyCleanupRequired
	}
	if entry.Completed {
		return Connection{}, false, nil
	}
	connection := record.Connection
	connection.PendingCleanup = connection.PendingCleanup || record.TokenDeleted || record.OAuthRevoked || record.KeepAuthorization || entry.CleanupOnly
	if entry.Imported {
		if state, err := r2connect.LoadState(r2connect.FileStateStore{Path: legacyStatePath(path, id)}); err == nil && !state.Authorized && len(state.Connections) == 0 && len(state.RetiredTokens) == 0 {
			connection.PendingCleanup = true
		}
	}
	return connection, true, nil
}

func legacyBlobManifest(store r2connect.SecretStore, id string) (legacyManifest, error) {
	var manifest legacyManifest
	if !legacyIDPattern.MatchString(id) {
		return manifest, errors.New("invalid legacy credential reference")
	}
	data, err := store.Load(id)
	if err != nil {
		return manifest, err
	}
	defer clear(data)
	if json.Unmarshal(data, &manifest) != nil || manifest.Parts < 1 || manifest.Parts > legacyMaxBlobBytes/legacyPartBytes+1 || !legacyHashPattern.MatchString(manifest.SHA256) {
		return manifest, errors.New("invalid legacy credential manifest")
	}
	return manifest, nil
}

// This is a compatibility reader only. All new writes and native/chunked
// storage belong to the SDK's SecretStore implementation.
func readLegacyBlob(ctx context.Context, store r2connect.SecretStore, id string, value any) error {
	if store == nil {
		return keyring.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	manifest, err := legacyBlobManifest(store, id)
	if err != nil {
		return err
	}
	var joined []byte
	defer func() { clear(joined) }()
	for index := 0; index < manifest.Parts; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		part, err := store.Load(id + "." + strconv.Itoa(index))
		if err != nil {
			return err
		}
		joined = append(joined, part...)
		clear(part)
		if len(joined) > legacyMaxBlobBytes {
			return errors.New("legacy credentials exceed their storage limit")
		}
	}
	digest := sha256.Sum256(joined)
	if hex.EncodeToString(digest[:]) != manifest.SHA256 {
		return errors.New("legacy credentials are incomplete")
	}
	if json.Unmarshal(joined, value) != nil {
		return errors.New("legacy credentials are invalid")
	}
	return nil
}
func deleteLegacyBlob(ctx context.Context, store r2connect.SecretStore, id string) error {
	if id == "" {
		return nil
	}
	if store == nil {
		return keyring.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	manifest, err := legacyBlobManifest(store, id)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	for index := 0; index < manifest.Parts; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := store.Delete(id + "." + strconv.Itoa(index)); err != nil {
			return err
		}
	}
	return store.Delete(id)
}

type legacyStateView struct{ state r2connect.State }

func (s legacyStateView) Load() (r2connect.State, bool, error) { return s.state, true, nil }
func (s legacyStateView) Save(r2connect.State) error {
	return errors.New("read-only migration validation")
}

func legacyTerminal(record legacyRecord) bool {
	return record.OAuthRevoked || record.KeepAuthorization && record.TokenDeleted
}
func legacyHasRemoteRetired(record legacyRecord) bool {
	for _, retired := range record.Retired {
		if retired.TokenID != "" {
			return true
		}
	}
	return false
}

// MigrateLegacy is called for this one connection on use or explicit disconnect,
// with the application's cross-process metadata lock already held.
func MigrateLegacy(ctx context.Context, path, id string, sdkSecrets r2connect.SecretStore) error {
	return migrateLegacyWithStore(ctx, path, id, sdkSecrets, legacySecrets(path))
}
func migrateLegacyWithStore(ctx context.Context, path, id string, sdkSecrets, oldSecrets r2connect.SecretStore) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	metadata, err := loadLegacy(path)
	if err != nil {
		return err
	}
	record, ok := metadata.Connections[id]
	if !ok {
		return nil
	}
	journal, err := loadLegacyJournal(path)
	if err != nil {
		return err
	}
	entry := journal.Entries[id]
	if entry.Completed {
		return nil
	}
	sourceDigest := legacyDigest(record)
	if entry.SourceSHA256 != "" && entry.SourceSHA256 != sourceDigest {
		return ErrLegacyCleanupRequired
	}
	stateStore := r2connect.FileStateStore{Path: legacyStatePath(path, id)}
	if entry.Imported {
		_, exists, err := stateStore.Load()
		if err != nil {
			return err
		}
		if !exists {
			return ErrLegacyCleanupRequired
		}
		_, err = r2connect.LoadState(stateStore)
		return err
	}
	if entry.StateSHA256 != "" {
		if saved, exists, err := stateStore.Load(); err != nil {
			return err
		} else if exists {
			if legacyDigest(saved) != entry.StateSHA256 {
				return ErrLegacyCleanupRequired
			}
			if _, err := r2connect.LoadState(stateStore); err != nil {
				return err
			}
			entry.Imported = true
			journal.Entries[id] = entry
			return saveLegacyJournal(path, journal)
		}
	}
	if _, exists, err := stateStore.Load(); err != nil {
		return err
	} else if exists {
		return ErrLegacyCleanupRequired
	}
	if legacyTerminal(record) {
		if legacyHasRemoteRetired(record) {
			return ErrLegacyCleanupRequired
		}
		return ErrLegacyLocalOnly
	}
	if record.KeepAuthorization {
		return ErrLegacyCleanupRequired
	}
	if err := ensureLegacyDirectory(path); err != nil {
		return err
	}
	var old legacySecretsRecord
	if err := readLegacyBlob(ctx, oldSecrets, record.SecretID, &old); err != nil {
		if errors.Is(err, keyring.ErrAccessDenied) || errors.Is(err, keyring.ErrUnavailable) || ctx.Err() != nil {
			return err
		}
		return fmt.Errorf("%w：无法读取此连接的旧凭据", ErrLegacyCleanupRequired)
	}
	if old.OAuth.AccessToken == "" || old.OAuth.ExpiresAt.IsZero() {
		return ErrLegacyCleanupRequired
	}
	cleanupOnly := record.PendingCleanup || record.TokenDeleted
	if cleanupOnly && old.OAuth.RefreshToken == "" && !old.OAuth.ExpiresAt.After(time.Now()) {
		return ErrLegacyCleanupRequired
	}
	oauthKey := "oauth-legacy-" + id + "-" + sourceDigest[:12]
	credentialKey := "connection-legacy-" + id + "-" + record.TokenID
	state := r2connect.State{Version: 1, ClientID: record.ClientID, Authorized: true, Connections: []r2connect.Connection{}, OAuthCredentialKey: oauthKey, Renewable: old.OAuth.RefreshToken != ""}
	if !cleanupOnly {
		endpoint, err := r2connect.Endpoint(record.AccountID, record.Jurisdiction)
		if err != nil || record.Endpoint != endpoint || old.AccessKey == "" || old.SecretKey == "" {
			return ErrLegacyCleanupRequired
		}
		state.Connections = append(state.Connections, r2connect.Connection{ID: id, AccountID: record.AccountID, Bucket: record.Bucket, Jurisdiction: record.Jurisdiction, TokenID: record.TokenID, ExpiresAt: record.ExpiresAt, CredentialKey: credentialKey})
	}
	seen := map[string]bool{}
	if !cleanupOnly {
		seen[record.TokenID] = true
	}
	appendRetired := func(tokenID string) {
		if tokenID != "" && !seen[tokenID] {
			state.RetiredTokens = append(state.RetiredTokens, r2connect.RetiredToken{AccountID: record.AccountID, TokenID: tokenID})
			seen[tokenID] = true
		}
	}
	if record.PendingCleanup && !record.TokenDeleted {
		appendRetired(record.TokenID)
	}
	for _, retired := range record.Retired {
		appendRetired(retired.TokenID)
	}
	if _, err := r2connect.LoadState(legacyStateView{state}); err != nil {
		return fmt.Errorf("%w：旧连接格式不受 SDK 支持", ErrLegacyCleanupRequired)
	}
	if err := ensureLegacyDirectory(path); err != nil {
		return err
	}
	stateDigest := legacyDigest(state)
	if saved, exists, err := stateStore.Load(); err != nil {
		return err
	} else if exists {
		if entry.SourceSHA256 == sourceDigest && entry.StateSHA256 == stateDigest && legacyDigest(saved) == stateDigest {
			entry.Imported = true
			journal.Entries[id] = entry
			return saveLegacyJournal(path, journal)
		}
		return ErrLegacyCleanupRequired
	}
	entry = legacyImportEntry{SourceSHA256: sourceDigest, StateSHA256: stateDigest, CleanupOnly: cleanupOnly}
	journal.Entries[id] = entry
	if err := saveLegacyJournal(path, journal); err != nil {
		return err
	}
	var written []string
	rollback := func() {
		for _, key := range written {
			_ = sdkSecrets.Delete(key)
		}
	}
	save := func(key string, value any) error {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		defer clear(encoded)
		if err := sdkSecrets.Save(key, encoded); err != nil {
			return err
		}
		written = append(written, key)
		verified, err := sdkSecrets.Load(key)
		if err != nil {
			return err
		}
		defer clear(verified)
		if string(verified) != string(encoded) {
			return errors.New("SDK 迁移凭据校验失败，旧凭据已保留")
		}
		return ctx.Err()
	}
	if err := save(oauthKey, r2connect.TokenSet{AccessToken: old.OAuth.AccessToken, RefreshToken: old.OAuth.RefreshToken, Scope: old.OAuth.Scope, ExpiresAt: old.OAuth.ExpiresAt}); err != nil {
		rollback()
		return err
	}
	if !cleanupOnly {
		if err := save(credentialKey, r2connect.Credentials{TokenID: record.TokenID, AccessKeyID: old.AccessKey, SecretAccessKey: old.SecretKey, ExpiresAt: record.ExpiresAt}); err != nil {
			rollback()
			return err
		}
	}
	if err := stateStore.Save(state); err != nil {
		rollback()
		return err
	}
	if _, err := r2connect.LoadState(stateStore); err != nil {
		return err
	}
	entry.Imported = true
	journal.Entries[id] = entry
	return saveLegacyJournal(path, journal)
}

// FinishLegacy is explicit Disconnect cleanup only. It proves that remote work
// finished before deleting any old local credential; source metadata is retained.
func FinishLegacy(ctx context.Context, path, id string) error {
	metadata, err := loadLegacy(path)
	if err != nil {
		return err
	}
	if _, exists := metadata.Connections[id]; !exists {
		return nil
	}
	store := legacySecrets(path)
	store.RetryAccess()
	return finishLegacyWithStore(ctx, path, id, store)
}
func finishLegacyWithStore(ctx context.Context, path, id string, oldSecrets r2connect.SecretStore) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	metadata, err := loadLegacy(path)
	if err != nil {
		return err
	}
	record, ok := metadata.Connections[id]
	if !ok {
		return nil
	}
	journal, err := loadLegacyJournal(path)
	if err != nil {
		return err
	}
	entry := journal.Entries[id]
	if entry.Completed {
		return nil
	}
	sourceDigest := legacyDigest(record)
	if entry.SourceSHA256 != "" && entry.SourceSHA256 != sourceDigest {
		return ErrLegacyCleanupRequired
	}
	if legacyTerminal(record) {
		if legacyHasRemoteRetired(record) {
			return ErrLegacyCleanupRequired
		}
	} else {
		if !entry.Imported {
			return ErrLegacyCleanupRequired
		}
		store := r2connect.FileStateStore{Path: legacyStatePath(path, id)}
		_, exists, err := store.Load()
		if err != nil {
			return err
		}
		if !exists {
			return ErrLegacyCleanupRequired
		}
		state, err := r2connect.LoadState(store)
		if err != nil {
			return err
		}
		if state.Authorized || len(state.Connections) > 0 || len(state.RetiredTokens) > 0 {
			return errors.New("SDK 尚未完成远端断开，旧凭据已保留")
		}
	}
	if err := ensureLegacyDirectory(path); err != nil {
		return err
	}
	refs := map[string]bool{}
	if record.SecretID != "" {
		refs[record.SecretID] = true
	}
	for _, retired := range record.Retired {
		if retired.SecretID != "" {
			refs[retired.SecretID] = true
		}
	}
	for otherID, other := range metadata.Connections {
		if otherID == id || journal.Entries[otherID].Completed {
			continue
		}
		delete(refs, other.SecretID)
		for _, retired := range other.Retired {
			delete(refs, retired.SecretID)
		}
	}
	for ref := range refs {
		if err := deleteLegacyBlob(ctx, oldSecrets, ref); err != nil {
			return err
		}
	}
	entry.SourceSHA256 = sourceDigest
	entry.Completed = true
	journal.Entries[id] = entry
	return saveLegacyJournal(path, journal)
}
