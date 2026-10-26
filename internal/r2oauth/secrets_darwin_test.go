//go:build darwin && cgo

package r2oauth

import (
	"github.com/knothhe/r2connect/keyring"
	"testing"
)

// Constructing a backend does not load, save or delete real credentials.
func TestSDKNativeKeyringConstructionDoesNotAccessCredentials(t *testing.T) {
	if !systemStoreAvailable() {
		t.Fatal("macOS cgo build disabled its native backend")
	}
	if store := keyring.New("com.knothhe.picup.test.not-accessed", "Picup tests"); store == nil {
		t.Fatal("SDK native keyring construction failed")
	}
}
