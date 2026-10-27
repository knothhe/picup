package r2oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/knothhe/r2connect"
)

func TestSDKSecretSessionObservesOAuthRefreshWithoutPublicStateChange(t *testing.T) {
	for _, kind := range []string{"sdk", "legacy"} {
		t.Run(kind, func(t *testing.T) {
			f := newSecretSessionIntegration(t)
			state := f.state(t)
			data, err := f.secrets.Load(state.OAuthCredentialKey)
			if err != nil {
				t.Fatal(err)
			}
			var tokens r2connect.TokenSet
			err = json.Unmarshal(data, &tokens)
			clear(data)
			if err != nil {
				t.Fatal(err)
			}
			tokens.ExpiresAt = time.Now().Add(-time.Minute)
			tokens.RefreshToken = "fixture-refresh-before-rotation"
			if kind == "legacy" {
				state.OAuthCredentialKey = "oauth-legacy-" + f.sessionID + "-abcdefabcdef"
			}
			data, err = json.Marshal(tokens)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.secrets.Save(state.OAuthCredentialKey, data); err != nil {
				t.Fatal(err)
			}
			clear(data)
			path := f.manager.sessionPath(f.sessionID)
			stateStore := r2connect.FileStateStore{Path: path}
			if err := stateStore.Save(state); err != nil {
				t.Fatal(err)
			}
			session, err := f.manager.session(f.sessionID)
			if err != nil {
				t.Fatal(err)
			}
			before, err := f.store.Load(state.OAuthCredentialKey)
			if err != nil {
				t.Fatal(err)
			}
			defer clear(before)
			publicBefore, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			// A separate SDK consumer refreshes OAuth in place. The SDK writes
			// the same secret key but does not update public session metadata.
			other := r2connect.NewManager(session.client, r2connect.ManagerOptions{
				Secrets: f.secrets, State: stateStore,
			})
			if _, err := other.Accounts(context.Background()); err != nil {
				t.Fatal(err)
			}
			publicAfter, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(publicBefore, publicAfter) {
				t.Fatal("fixture unexpectedly changed public metadata during OAuth refresh")
			}
			latest, err := f.secrets.Load(state.OAuthCredentialKey)
			if err != nil {
				t.Fatal(err)
			}
			defer clear(latest)
			if bytes.Equal(before, latest) {
				t.Fatal("fixture did not rotate the OAuth grant")
			}
			if _, err := f.manager.session(f.sessionID); err != nil {
				t.Fatal(err)
			}
			observed, err := f.store.Load(state.OAuthCredentialKey)
			if err != nil {
				t.Fatal(err)
			}
			defer clear(observed)
			if !bytes.Equal(observed, latest) {
				t.Fatal("the connection session reused OAuth credentials from before another process refreshed them")
			}
			if f.loads() != 2 {
				t.Fatalf("OAuth reads must both reach the native store, got %d", f.loads())
			}
		})
	}
}
