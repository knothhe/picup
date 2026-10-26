package r2oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/knothhe/r2connect"
)

func routeFixture(t *testing.T) (string, string, Connection) {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "config.json")
	sid := strings.Repeat("a", 32)
	id, _, err := r2connect.NewPKCE()
	if err != nil {
		t.Fatal(err)
	}
	account := strings.Repeat("b", 32)
	endpoint, _ := r2connect.Endpoint(account, "default")
	return path, sid, Connection{ID: id, AccountID: account, AccountName: "Fixture account", Jurisdiction: "default", Bucket: "photos", Endpoint: endpoint, PublicURL: "https://images.example.com", ExpiresAt: time.Now().UTC().Add(30 * 24 * time.Hour), Renewable: true}
}

func TestSessionRoutesPreservePublicSnapshotAcrossLocalCleanupFailure(t *testing.T) {
	path, sid, connection := routeFixture(t)
	missing, err := loadSessionRoutes(path, sid)
	if err != nil || missing == nil || len(missing) != 0 {
		t.Fatal(missing, err)
	}
	if err := saveSessionRoute(path, sid, connection); err != nil {
		t.Fatal(err)
	}
	routes, err := loadSessionRoutes(path, sid)
	if err != nil || !reflect.DeepEqual(routes[connection.ID], connection) {
		t.Fatal(routes, err)
	}
	filename, _ := sessionRoutePath(path, sid)
	info, err := os.Stat(filename)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	data, _ := os.ReadFile(filename)
	for _, forbidden := range []string{"token_id", "tokenId", "credentialKey", "accessToken", "secretAccessKey", "access_key", "secret_key"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatal("private SDK field in route", forbidden)
		}
	}
	// The SDK file can be gone/empty after remote cleanup; this public route is
	// still independently readable by the application's cleanup path.
	if routes[connection.ID].ID != connection.ID || routes[connection.ID].Bucket != "photos" {
		t.Fatal("lost cleanup identity")
	}
	if err := removeSessionRoute(path, sid, connection.ID); err != nil {
		t.Fatal(err)
	}
	if err := removeSessionRoutes(path, sid); err != nil {
		t.Fatal("repeat removal", err)
	}
	if routes, err := loadSessionRoutes(path, sid); err != nil || routes == nil || len(routes) != 0 {
		t.Fatal(routes, err)
	}
}

func TestSessionRoutesBindFileSessionMapKeyAndStorageDestination(t *testing.T) {
	path, sid, connection := routeFixture(t)
	if err := saveSessionRoute(path, sid, connection); err != nil {
		t.Fatal(err)
	}
	filename, _ := sessionRoutePath(path, sid)
	before, _ := os.ReadFile(filename)
	rebound := connection
	rebound.Bucket = "other-photos"
	if err := saveSessionRoute(path, sid, rebound); err == nil {
		t.Fatal("rebound same connection ID to another bucket")
	}
	if after, _ := os.ReadFile(filename); string(before) != string(after) {
		t.Fatal("failed rebind modified original route")
	}
	for _, kind := range []string{"session", "map-key", "endpoint", "unknown-field", "version", "empty"} {
		t.Run(kind, func(t *testing.T) {
			state := sessionRouteState{Version: 1, SessionID: sid, Connections: map[string]Connection{connection.ID: connection}}
			switch kind {
			case "session":
				state.SessionID = strings.Repeat("c", 32)
			case "map-key":
				delete(state.Connections, connection.ID)
				state.Connections[strings.Repeat("d", 32)] = connection
			case "endpoint":
				bad := connection
				bad.Endpoint = "https://foreign.example.com"
				state.Connections[connection.ID] = bad
			case "version":
				state.Version = 2
			}
			data, _ := json.Marshal(state)
			if kind == "unknown-field" {
				data = []byte(strings.TrimSuffix(string(data), "}") + `,"access_token":"must-not-be-loaded"}`)
			}
			if kind == "empty" {
				data = nil
			}
			if err := os.WriteFile(filename, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadSessionRoutes(path, sid); err == nil {
				t.Fatal("accepted corrupt or misbound route")
			}
			if err := saveSessionRoute(path, sid, connection); err == nil {
				t.Fatal("overwrote corrupt route as first initialization")
			}
			if err := removeSessionRoutes(path, sid); err == nil {
				t.Fatal("deleted unknown or misbound route")
			}
			if after, _ := os.ReadFile(filename); string(after) != string(data) {
				t.Fatal("corrupt route was not preserved")
			}
		})
	}
}

func TestSessionRoutesBoundedAndSingleRemovalKeepsOtherConnections(t *testing.T) {
	path, sid, connection := routeFixture(t)
	var ids []string
	for index := 0; index < sessionRouteLimit; index++ {
		entry := connection
		entry.ID = fmt.Sprintf("%032x", index+1)
		ids = append(ids, entry.ID)
		if err := saveSessionRoute(path, sid, entry); err != nil {
			t.Fatal(err)
		}
	}
	extra := connection
	extra.ID = strings.Repeat("f", 32)
	if err := saveSessionRoute(path, sid, extra); !errors.Is(err, r2connect.ErrTooManyConnections) {
		t.Fatal("route limit was ignored", err)
	}
	if err := removeSessionRoute(path, sid, ids[0]); err != nil {
		t.Fatal(err)
	}
	routes, err := loadSessionRoutes(path, sid)
	if err != nil || len(routes) != sessionRouteLimit-1 {
		t.Fatal(len(routes), err)
	}
	if _, ok := routes[ids[1]]; !ok {
		t.Fatal("single discard removed a different profile route")
	}
}

func TestSessionRoutesRejectSymlinksAndUnsafePermissions(t *testing.T) {
	for _, kind := range []string{"file-symlink", "directory-symlink", "permissions"} {
		t.Run(kind, func(t *testing.T) {
			path, sid, connection := routeFixture(t)
			if err := saveSessionRoute(path, sid, connection); err != nil {
				t.Fatal(err)
			}
			filename, _ := sessionRoutePath(path, sid)
			original, _ := os.ReadFile(filename)
			outside := filepath.Join(filepath.Dir(path), "outside")
			switch kind {
			case "file-symlink":
				os.WriteFile(outside, original, 0600)
				os.Remove(filename)
				os.Symlink(outside, filename)
			case "directory-symlink":
				os.Rename(filepath.Dir(filename), outside)
				os.Symlink(outside, filepath.Dir(filename))
			case "permissions":
				os.Chmod(filename, 0644)
			}
			if _, err := loadSessionRoutes(path, sid); err == nil {
				t.Fatal("accepted unsafe route target")
			}
			if err := saveSessionRoute(path, sid, connection); err == nil {
				t.Fatal("overwrote unsafe route target")
			}
			if err := removeSessionRoutes(path, sid); err == nil {
				t.Fatal("removed unsafe route target")
			}
			if kind == "file-symlink" {
				if data, _ := os.ReadFile(outside); string(data) != string(original) {
					t.Fatal("changed symlink target")
				}
			}
		})
	}
}

func TestSessionRoutesAtomicReplacementNeverPublishesPartialJSON(t *testing.T) {
	path, sid, connection := routeFixture(t)
	if err := saveSessionRoute(path, sid, connection); err != nil {
		t.Fatal(err)
	}
	var readers sync.WaitGroup
	stop := make(chan struct{})
	failures := make(chan error, 4)
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if routes, err := loadSessionRoutes(path, sid); err != nil {
					select {
					case failures <- err:
					default:
					}
					return
				} else if len(routes) != 1 {
					select {
					case failures <- errors.New("partial route map"):
					default:
					}
					return
				}
			}
		}()
	}
	for index := 0; index < 30; index++ {
		connection.AccountName = fmt.Sprintf("Fixture account %d", index)
		if err := saveSessionRoute(path, sid, connection); err != nil {
			close(stop)
			readers.Wait()
			t.Fatal(err)
		}
	}
	close(stop)
	readers.Wait()
	select {
	case err := <-failures:
		t.Fatal("reader observed incomplete atomic replacement", err)
	default:
	}
}
