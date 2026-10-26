package r2oauth

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/knothhe/r2connect"
	"github.com/knothhe/r2connect/loopback"
)

const cloudflareDiagnosticLimit int64 = 1 << 20
const cloudflareDiagnosticName = "cloudflare.jsonl"

// Keep serialization local and explicit. SDK additions must never silently
// widen the data that Picup persists. A request result is not an API result.
type cloudflareDiagnosticRecord struct {
	Version              int       `json:"version"`
	Time                 time.Time `json:"time"`
	Stage                string    `json:"stage"`
	Phase                string    `json:"phase,omitempty"`
	Result               string    `json:"result,omitempty"`
	Method               string    `json:"method,omitempty"`
	Status               int       `json:"status,omitempty"`
	Codes                []int     `json:"codes,omitempty"`
	Ray                  string    `json:"ray,omitempty"`
	DurationMillis       int64     `json:"duration_millis,omitempty"`
	PermissionIDLength   int       `json:"permission_id_length,omitempty"`
	PermissionIDValid    *bool     `json:"permission_id_valid,omitempty"`
	PermissionSelectable *bool     `json:"permission_selectable,omitempty"`
	PermissionScopeValid *bool     `json:"permission_scope_valid,omitempty"`
	MethodMatches        *bool     `json:"method_matches,omitempty"`
	HostMatches          *bool     `json:"host_matches,omitempty"`
	PathMatches          *bool     `json:"path_matches,omitempty"`
	StatePresent         *bool     `json:"state_present,omitempty"`
	StateMatches         *bool     `json:"state_matches,omitempty"`
	CodePresent          *bool     `json:"code_present,omitempty"`
	OAuthErrorPresent    *bool     `json:"oauth_error_present,omitempty"`
	OAuthError           string    `json:"oauth_error,omitempty"`
}

var cloudflareDiagnosticMu sync.Mutex
var diagnosticRayPattern = regexp.MustCompile(`^[0-9a-fA-F]{16}(-[A-Z]{3})?$`)

func cloudflareDiagnosticsSink(configPath string) func(r2connect.DiagnosticEvent) {
	directory := filepath.Join(filepath.Dir(canonicalConfigPath(configPath)), "logs")
	return func(event r2connect.DiagnosticEvent) {
		if record, ok := cloudflareLogRecord(event); ok {
			persistCloudflareDiagnostic(directory, record)
		}
	}
}

func loopbackDiagnosticsSink(configPath string) func(loopback.Event) {
	directory := filepath.Join(filepath.Dir(canonicalConfigPath(configPath)), "logs")
	return func(event loopback.Event) {
		if record, ok := loopbackLogRecord(event); ok {
			persistCloudflareDiagnostic(directory, record)
		}
	}
}

func persistCloudflareDiagnostic(directory string, record cloudflareDiagnosticRecord) {
	encoded, err := json.Marshal(record)
	if err != nil {
		return
	}
	// Logging is best effort, including permission, full-disk and unsafe-path
	// failures. It must never change an authorization operation's result.
	_ = appendCloudflareDiagnostic(directory, append(encoded, '\n'))
}

func cloudflareLogRecord(event r2connect.DiagnosticEvent) (cloudflareDiagnosticRecord, bool) {
	var stage string
	switch event.Operation {
	case "exchange Cloudflare authorization":
		stage = "oauth.exchange"
	case "revoke Cloudflare authorization":
		stage = "oauth.revoke"
	case "list Cloudflare accounts":
		stage = "accounts"
	case "list R2 buckets":
		stage = "buckets"
	case "read R2 credential permissions":
		stage = "permissions"
	case "create R2 credentials":
		stage = "credentials.create"
	case "delete R2 credentials":
		stage = "credentials.delete"
	case "Cloudflare request":
		stage = "api"
	default:
		return cloudflareDiagnosticRecord{}, false
	}
	switch event.Phase {
	case "request_started":
		stage += ".request"
	case "request_completed":
		stage += ".response"
	case "api_result":
		stage += ".result"
	case "permission_group":
		stage += ".group"
	default:
		return cloudflareDiagnosticRecord{}, false
	}
	switch event.Result {
	case "pending", "success", "http_error", "api_error", "network_error", "canceled", "invalid_response", "response_too_large", "read_error", "invalid_request", "advisory":
	default:
		return cloudflareDiagnosticRecord{}, false
	}
	method := ""
	switch event.Method {
	case "GET", "POST", "DELETE", "OTHER":
		method = event.Method
	}
	status := event.HTTPStatus
	if status < 100 || status > 599 {
		status = 0
	}
	var codes []int
	for _, code := range event.ErrorCodes {
		if code > 0 && code <= 1_000_000 {
			codes = append(codes, code)
		}
		if len(codes) == 20 {
			break
		}
	}
	ray := ""
	if diagnosticRayPattern.MatchString(event.CFRay) {
		ray = event.CFRay
	}
	return cloudflareDiagnosticRecord{
		Version: 2, Time: time.Now().UTC(), Stage: stage, Phase: event.Phase,
		Result: event.Result, Method: method, Status: status, Codes: codes, Ray: ray,
		DurationMillis:     min(max(event.DurationMS, 0), int64(24*time.Hour/time.Millisecond)),
		PermissionIDLength: min(max(event.PermissionIDLength, 0), 256),
		PermissionIDValid:  event.PermissionIDValid, PermissionSelectable: event.PermissionSelectable,
		PermissionScopeValid: event.PermissionScopeValid,
	}, true
}

func loopbackLogRecord(event loopback.Event) (cloudflareDiagnosticRecord, bool) {
	switch event.Stage {
	case "callback_rejected", "callback_received", "code_exchange_started", "code_exchange_finished", "listener_error", "context_done", "listener_closed":
	default:
		return cloudflareDiagnosticRecord{}, false
	}
	switch event.Result {
	case "", "invalid_request", "invalid_state", "inactive", "invalid_code", "success", "failed", "canceled", "timeout":
	default:
		return cloudflareDiagnosticRecord{}, false
	}
	return cloudflareDiagnosticRecord{
		Version: 2, Time: time.Now().UTC(), Stage: "oauth." + event.Stage, Result: event.Result,
		MethodMatches: event.MethodMatches, HostMatches: event.HostMatches, PathMatches: event.PathMatches,
		StatePresent: event.StatePresent, StateMatches: event.StateMatches, CodePresent: event.CodePresent,
		OAuthErrorPresent: event.OAuthErrorPresent, OAuthError: loopback.ErrorCode(event.OAuthError),
	}, true
}

func diagnosticExistingFile(root *os.Root, name string) (os.FileInfo, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() < 0 || info.Size() > cloudflareDiagnosticLimit {
		return nil, errors.New("unsafe diagnostic file")
	}
	file, err := openDiagnosticFile(root, name, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, actual) {
		return nil, errors.New("diagnostic file changed during inspection")
	}
	return actual, nil
}

func appendCloudflareDiagnostic(directory string, encoded []byte) error {
	if len(encoded) == 0 || int64(len(encoded)) > cloudflareDiagnosticLimit {
		return errors.New("diagnostic event exceeds its limit")
	}
	cloudflareDiagnosticMu.Lock()
	defer cloudflareDiagnosticMu.Unlock()
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		return errors.New("unsafe diagnostic directory")
	}
	directoryFile, unlock, err := lockDiagnosticDirectory(directory)
	if err != nil {
		return err
	}
	defer unlock()
	lockedInfo, err := directoryFile.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, lockedInfo) {
		return errors.New("diagnostic directory changed during inspection")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	rootInfo, err := root.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(lockedInfo, rootInfo) {
		return errors.New("diagnostic root changed during inspection")
	}
	backup := cloudflareDiagnosticName + ".1"
	if _, err := diagnosticExistingFile(root, backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	current, err := diagnosticExistingFile(root, cloudflareDiagnosticName)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if current != nil && current.Size()+int64(len(encoded)) > cloudflareDiagnosticLimit {
		if err := root.Remove(backup); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := root.Rename(cloudflareDiagnosticName, backup); err != nil {
			return err
		}
		current = nil
	}
	flags := os.O_WRONLY | os.O_APPEND
	if current == nil {
		flags |= os.O_CREATE | os.O_EXCL
	}
	file, err := openDiagnosticFile(root, cloudflareDiagnosticName, flags)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	if current != nil && !os.SameFile(current, opened) {
		return errors.New("diagnostic file changed before append")
	}
	if opened.Size()+int64(len(encoded)) > cloudflareDiagnosticLimit {
		return errors.New("diagnostic file grew beyond its append limit")
	}
	written, err := file.Write(encoded)
	if err == nil && written != len(encoded) {
		err = io.ErrShortWrite
	}
	if err != nil {
		_ = file.Truncate(opened.Size())
	}
	return err
}
