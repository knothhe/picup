package r2oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/knothhe/r2connect"
	"github.com/knothhe/r2connect/loopback"
)

func diagnosticFixture(t *testing.T) (string, func(r2connect.DiagnosticEvent)) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("safe diagnostics currently use Unix ownership and file locks")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "logs"), cloudflareDiagnosticsSink(filepath.Join(root, "config.json"))
}

func diagnosticEvent() r2connect.DiagnosticEvent {
	return r2connect.DiagnosticEvent{Operation: "list Cloudflare accounts", Phase: "request_completed", Result: "http_error", Method: "GET", HTTPStatus: 429, ErrorCodes: []int{10058}, CFRay: "0123456789abcdef-SJC", DurationMS: 40}
}

func diagnosticLines(t *testing.T, path string) []cloudflareDiagnosticRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var events []cloudflareDiagnosticRecord
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		var event cloudflareDiagnosticRecord
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("invalid JSONL: %v", err)
		}
		events = append(events, event)
	}
	return events
}

func TestDiagnosticLogClosedSchemaPermissionsAndSDKFields(t *testing.T) {
	directory, sink := diagnosticFixture(t)
	before := time.Now().Add(-time.Second)
	sink(diagnosticEvent())
	path := filepath.Join(directory, cloudflareDiagnosticName)
	for target, want := range map[string]os.FileMode{directory: 0700, path: 0600} {
		info, err := os.Stat(target)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("%s permission: %v %v", target, info, err)
		}
	}
	events := diagnosticLines(t, path)
	if len(events) != 1 {
		t.Fatal(events)
	}
	event := events[0]
	if event.Version != 2 || event.Stage != "accounts.response" || event.Phase != "request_completed" || event.Result != "http_error" || event.Method != "GET" || event.Status != 429 || event.Ray != "0123456789abcdef-SJC" || len(event.Codes) != 1 || event.Codes[0] != 10058 || event.DurationMillis != 40 || event.Time.Before(before) || event.Time.Location() != time.UTC {
		t.Fatal(event)
	}
	data, _ := os.ReadFile(path)
	var fields map[string]json.RawMessage
	json.Unmarshal(bytes.TrimSpace(data), &fields)
	allowed := map[string]bool{"version": true, "time": true, "stage": true, "phase": true, "result": true, "method": true, "status": true, "codes": true, "ray": true, "duration_millis": true}
	for field := range fields {
		if !allowed[field] {
			t.Fatal("unexpected persistent field", field)
		}
	}
}

func TestDiagnosticLogRejectsFreeTextAndClampsNumericValues(t *testing.T) {
	directory, sink := diagnosticFixture(t)
	malicious := "Bearer user-secret https://example.invalid/?access_token=raw-secret\nbody=private"
	for _, field := range []string{"operation", "phase", "result"} {
		bad := diagnosticEvent()
		switch field {
		case "operation":
			bad.Operation = malicious
		case "phase":
			bad.Phase = malicious
		case "result":
			bad.Result = malicious
		}
		sink(bad)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("unsafe free text created diagnostic files")
	}
	bad := diagnosticEvent()
	bad.Method = malicious
	bad.CFRay = malicious
	bad.HTTPStatus = 999
	bad.ErrorCodes = append([]int{-1, 2_000_000}, make([]int, 30)...)
	bad.DurationMS = 1 << 62
	bad.PermissionIDLength = 1 << 30
	sink(bad)
	data, err := os.ReadFile(filepath.Join(directory, cloudflareDiagnosticName))
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"user-secret", "raw-secret", "https://", "body=private"} {
		if bytes.Contains(data, []byte(private)) {
			t.Fatal("private input persisted", private)
		}
	}
	event := diagnosticLines(t, filepath.Join(directory, cloudflareDiagnosticName))[0]
	if event.Method != "" || event.Ray != "" || event.Status != 0 || len(event.Codes) != 0 || event.DurationMillis != 24*60*60*1000 || event.PermissionIDLength != 256 {
		t.Fatal(event)
	}
	bad.DurationMS = -1
	record, _ := cloudflareLogRecord(bad)
	if record.DurationMillis != 0 {
		t.Fatal("negative duration retained", record)
	}
}

func TestDiagnosticLogRotationKeepsOnlyTwoBoundedGenerations(t *testing.T) {
	directory, sink := diagnosticFixture(t)
	sink(diagnosticEvent())
	path := filepath.Join(directory, cloudflareDiagnosticName)
	seed := bytes.Repeat([]byte("{}\n"), int(cloudflareDiagnosticLimit)/3)
	for range 2 {
		if err := os.WriteFile(path, seed, 0600); err != nil {
			t.Fatal(err)
		}
		sink(diagnosticEvent())
		for _, name := range []string{cloudflareDiagnosticName, cloudflareDiagnosticName + ".1"} {
			info, err := os.Stat(filepath.Join(directory, name))
			if err != nil || info.Size() > cloudflareDiagnosticLimit || info.Mode().Perm() != 0600 {
				t.Fatal(name, info, err)
			}
		}
		if data, err := os.ReadFile(path + ".1"); err != nil || !bytes.Equal(data, seed) {
			t.Fatal("rotation did not preserve the previous bounded generation", err)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 2 {
		t.Fatal("extra generation or lock file", entries, err)
	}
}

func TestDiagnosticLogConcurrentCallbacksDoNotInterleave(t *testing.T) {
	directory, sink := diagnosticFixture(t)
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 30 {
				sink(diagnosticEvent())
			}
		}()
	}
	workers.Wait()
	events := diagnosticLines(t, filepath.Join(directory, cloudflareDiagnosticName))
	if len(events) != 240 {
		t.Fatal("concurrent diagnostic events were lost or interleaved", len(events))
	}
}

func TestDiagnosticLogUnsafeTargetsStayUntouched(t *testing.T) {
	for _, target := range []string{"directory-symlink", "current-symlink", "backup-symlink", "current-hardlink", "current-directory", "bad-directory-permission", "bad-file-permission"} {
		t.Run(target, func(t *testing.T) {
			directory, sink := diagnosticFixture(t)
			external := filepath.Join(filepath.Dir(directory), "keep-private")
			os.WriteFile(external, []byte("keep-sensitive-data"), 0600)
			if target == "directory-symlink" {
				os.Symlink(filepath.Dir(external), directory)
			} else {
				os.Mkdir(directory, 0700)
				path := filepath.Join(directory, cloudflareDiagnosticName)
				switch target {
				case "current-symlink":
					os.Symlink(external, path)
				case "backup-symlink":
					os.Symlink(external, path+".1")
				case "current-hardlink":
					if err := os.Link(external, path); err != nil {
						t.Skip(err)
					}
				case "current-directory":
					os.Mkdir(path, 0700)
				case "bad-directory-permission":
					os.Chmod(directory, 0755)
				case "bad-file-permission":
					os.WriteFile(path, []byte("existing-private-log"), 0644)
				}
			}
			sink(diagnosticEvent())
			if data, err := os.ReadFile(external); err != nil || string(data) != "keep-sensitive-data" {
				t.Fatal("unsafe target changed external file", err)
			}
			if target == "bad-file-permission" {
				data, _ := os.ReadFile(filepath.Join(directory, cloudflareDiagnosticName))
				if string(data) != "existing-private-log" {
					t.Fatal("wrote to unsafe-permission file")
				}
			}
			if target == "directory-symlink" {
				if _, err := os.Stat(filepath.Join(filepath.Dir(external), cloudflareDiagnosticName)); !os.IsNotExist(err) {
					t.Fatal("followed log directory symlink")
				}
			}
		})
	}
}

func TestDiagnosticLogLockFailureIsBestEffort(t *testing.T) {
	directory, sink := diagnosticFixture(t)
	os.Mkdir(directory, 0700)
	_, unlock, err := lockDiagnosticDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	sink(diagnosticEvent())
	if _, err := os.Stat(filepath.Join(directory, cloudflareDiagnosticName)); !os.IsNotExist(err) {
		t.Fatal("wrote through another writer's directory lock")
	}
}

func TestDiagnosticSDKStageFamiliesAndPermissionAdvisories(t *testing.T) {
	operations := map[string]string{"exchange Cloudflare authorization": "oauth.exchange", "revoke Cloudflare authorization": "oauth.revoke", "list Cloudflare accounts": "accounts", "list R2 buckets": "buckets", "read R2 credential permissions": "permissions", "create R2 credentials": "credentials.create", "delete R2 credentials": "credentials.delete", "Cloudflare request": "api"}
	phases := map[string]string{"request_started": "request", "request_completed": "response", "permission_group": "group", "api_result": "result"}
	for operation, prefix := range operations {
		for phase, suffix := range phases {
			event := diagnosticEvent()
			event.Operation = operation
			event.Phase = phase
			record, ok := cloudflareLogRecord(event)
			if !ok || record.Stage != prefix+"."+suffix {
				t.Fatal(operation, phase, record, ok)
			}
		}
	}
	yes, no := true, false
	event := diagnosticEvent()
	event.Operation = "read R2 credential permissions"
	event.Phase = "permission_group"
	event.Result = "advisory"
	event.PermissionIDLength = 32
	event.PermissionIDValid = &yes
	event.PermissionSelectable = &no
	event.PermissionScopeValid = &yes
	record, ok := cloudflareLogRecord(event)
	if !ok || record.PermissionIDValid == nil || !*record.PermissionIDValid || record.PermissionSelectable == nil || *record.PermissionSelectable || record.PermissionScopeValid == nil || !*record.PermissionScopeValid {
		t.Fatal(record)
	}
}

func TestLoopbackDiagnosticClosedSchemaAndSecretFreeOAuthErrors(t *testing.T) {
	directory, _ := diagnosticFixture(t)
	sink := loopbackDiagnosticsSink(filepath.Join(filepath.Dir(directory), "config.json"))
	yes, no := true, false
	for _, stage := range []string{"callback_rejected", "callback_received", "code_exchange_started", "code_exchange_finished", "listener_error", "context_done", "listener_closed"} {
		sink(loopback.Event{Stage: stage, Result: "", MethodMatches: &yes, HostMatches: &yes, PathMatches: &yes, StatePresent: &yes, StateMatches: &no, CodePresent: &no, OAuthErrorPresent: &yes, OAuthError: "access_denied"})
	}
	sink(loopback.Event{Stage: "private-code-state-token"})
	sink(loopback.Event{Stage: "callback_received", Result: "private-code-state-token"})
	sink(loopback.Event{Stage: "callback_received", OAuthError: "private-code-state-token"})
	events := diagnosticLines(t, filepath.Join(directory, cloudflareDiagnosticName))
	if len(events) != 8 {
		t.Fatal(events)
	}
	for _, event := range events[:7] {
		if !strings.HasPrefix(event.Stage, "oauth.") || event.OAuthError != "access_denied" || event.StateMatches == nil || *event.StateMatches {
			t.Fatal(event)
		}
	}
	if events[7].OAuthError != "other" {
		t.Fatal(events[7])
	}
	data, _ := os.ReadFile(filepath.Join(directory, cloudflareDiagnosticName))
	if bytes.Contains(data, []byte("private-code-state-token")) {
		t.Fatal("raw callback text persisted")
	}
}

type diagnosticTransportFunc func(*http.Request) (*http.Response, error)

func (f diagnosticTransportFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestSDKRealHTTPObserverRetainsStagesWithoutPrivateValues(t *testing.T) {
	directory, _ := diagnosticFixture(t)
	client := r2connect.NewClient("diagnostic-fixture")
	client.Diagnostic = cloudflareDiagnosticsSink(filepath.Join(filepath.Dir(directory), "config.json"))
	client.HTTPClient = &http.Client{Transport: diagnosticTransportFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != "GET" || request.URL.Path != "/client/v4/accounts" {
			t.Error("unexpected fixture request")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Cf-Ray": []string{"1020304050607080-LAX"}}, Body: io.NopCloser(strings.NewReader(`{"success":true,"result":[{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","name":"private-account-name"}]}`)), Request: request}, nil
	})}
	accounts, err := client.Accounts(context.Background(), "private-access-token")
	if err != nil || len(accounts) != 1 {
		t.Fatal("fixture request failed", err)
	}
	events := diagnosticLines(t, filepath.Join(directory, cloudflareDiagnosticName))
	requestSeen, responseSeen, resultSeen := false, false, false
	for _, event := range events {
		if event.Stage == "accounts.request" && event.Method == "GET" && event.Result == "pending" {
			requestSeen = true
		}
		if event.Stage == "accounts.response" && event.Status == 200 && event.Result == "success" && event.Ray == "1020304050607080-LAX" {
			responseSeen = true
		}
		if event.Stage == "accounts.result" && event.Result == "success" {
			resultSeen = true
		}
	}
	if !requestSeen || !responseSeen || !resultSeen {
		t.Fatal("SDK diagnostics were lost", events)
	}
	data, _ := os.ReadFile(filepath.Join(directory, cloudflareDiagnosticName))
	for _, private := range []string{"private-access-token", "private-account-name", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "api.cloudflare.com", "Authorization"} {
		if bytes.Contains(data, []byte(private)) {
			t.Fatal("private HTTP information persisted", private)
		}
	}
	otherRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(otherRoot, "logs"), []byte("keep-this-file"), 0600)
	client.Diagnostic = cloudflareDiagnosticsSink(filepath.Join(otherRoot, "config.json"))
	if _, err := client.Accounts(context.Background(), "private-access-token"); err != nil {
		t.Fatal("logging failure blocked request", err)
	}
	if data, err := os.ReadFile(filepath.Join(otherRoot, "logs")); err != nil || string(data) != "keep-this-file" {
		t.Fatal("unsafe log target changed", err)
	}
}
