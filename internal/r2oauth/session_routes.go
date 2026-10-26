package r2oauth

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/r2connect"
)

const sessionRouteLimit = 16
const sessionRouteMaxBytes = 64 << 10

var routeConnectionID = regexp.MustCompile(`^[A-Za-z0-9_-]{12,80}$`)

// This contains Picup's public snapshot, never r2connect.Connection (TokenID)
// or Credentials. It only preserves the route after SDK remote cleanup.
type sessionRouteState struct {
	Version     int                   `json:"version"`
	SessionID   string                `json:"session_id"`
	Connections map[string]Connection `json:"connections"`
}

func sessionRoutePath(path, sid string) (string, error) {
	if !sessionID(sid) {
		return "", r2connect.ErrInvalidState
	}
	return filepath.Join(canonicalConfigPath(path)+".r2connect", sid+".picup.json"), nil
}

func validRouteText(value string, limit int) bool {
	return len(value) <= limit && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}
func validateSessionRoute(connection Connection) error {
	if !routeConnectionID.MatchString(connection.ID) || !validRouteText(connection.AccountName, 512) || connection.Bucket == "" || !validRouteText(connection.Bucket, 63) || connection.ExpiresAt.IsZero() || connection.ExpiresAt.Year() < 1970 || connection.ExpiresAt.Year() > 9999 {
		return r2connect.ErrInvalidState
	}
	endpoint, err := r2connect.Endpoint(connection.AccountID, connection.Jurisdiction)
	if err != nil || endpoint != connection.Endpoint {
		return r2connect.ErrInvalidState
	}
	if connection.PublicURL != "" {
		parsed, err := url.Parse(connection.PublicURL)
		if err != nil || !validRouteText(connection.PublicURL, 2048) || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return r2connect.ErrInvalidState
		}
	}
	return nil
}

// loadSessionRoutes is metadata-only. A missing file returns an initialized
// empty map; an empty or malformed existing file remains an error.
func loadSessionRoutes(path, sid string) (map[string]Connection, error) {
	filename, err := sessionRoutePath(path, sid)
	if err != nil {
		return nil, err
	}
	if info, err := os.Lstat(filepath.Dir(filename)); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, r2connect.ErrInvalidState
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	info, err := os.Lstat(filename)
	if os.IsNotExist(err) {
		return map[string]Connection{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > sessionRouteMaxBytes {
		return nil, r2connect.ErrInvalidState
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	if len(data) > sessionRouteMaxBytes {
		return nil, r2connect.ErrInvalidState
	}
	var state sessionRouteState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return nil, r2connect.ErrInvalidState
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, r2connect.ErrInvalidState
	}
	if state.Version != 1 || state.SessionID != sid || state.Connections == nil || len(state.Connections) > sessionRouteLimit {
		return nil, r2connect.ErrInvalidState
	}
	for id, connection := range state.Connections {
		if id != connection.ID {
			return nil, r2connect.ErrInvalidState
		}
		if err := validateSessionRoute(connection); err != nil {
			return nil, err
		}
	}
	return state.Connections, nil
}

// Mutators are serialized by Manager's existing metadata lock. AtomicWrite
// keeps unlocked readers from ever seeing a partially replaced public file.
func saveSessionRoute(path, sid string, connection Connection) error {
	if err := validateSessionRoute(connection); err != nil {
		return err
	}
	filename, err := sessionRoutePath(path, sid)
	if err != nil {
		return err
	}
	routes, err := loadSessionRoutes(path, sid)
	if err != nil {
		return err
	}
	if old, exists := routes[connection.ID]; exists {
		if old.AccountID != connection.AccountID || old.Bucket != connection.Bucket || old.Jurisdiction != connection.Jurisdiction || old.Endpoint != connection.Endpoint {
			return errors.New("Cloudflare connection route cannot be rebound to another bucket")
		}
	} else if len(routes) >= sessionRouteLimit {
		return r2connect.ErrTooManyConnections
	}
	routes[connection.ID] = connection
	data, err := json.MarshalIndent(sessionRouteState{Version: 1, SessionID: sid, Connections: routes}, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > sessionRouteMaxBytes {
		return r2connect.ErrInvalidState
	}
	if err := ensureLegacyDirectory(path); err != nil {
		return err
	}
	return config.AtomicWrite(filename, append(data, '\n'), 0600)
}

func removeSessionRoute(path, sid, id string) error {
	if !routeConnectionID.MatchString(id) {
		return r2connect.ErrInvalidState
	}
	routes, err := loadSessionRoutes(path, sid)
	if err != nil {
		return err
	}
	if _, exists := routes[id]; !exists {
		return nil
	}
	delete(routes, id)
	if len(routes) == 0 {
		return removeSessionRoutes(path, sid)
	}
	filename, err := sessionRoutePath(path, sid)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(sessionRouteState{Version: 1, SessionID: sid, Connections: routes}, "", "  ")
	if err != nil {
		return err
	}
	return config.AtomicWrite(filename, append(data, '\n'), 0600)
}
func removeSessionRoutes(path, sid string) error {
	if _, err := loadSessionRoutes(path, sid); err != nil {
		return err
	}
	filename, err := sessionRoutePath(path, sid)
	if err != nil {
		return err
	}
	if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
