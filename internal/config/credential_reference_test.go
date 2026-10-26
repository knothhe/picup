package config

import (
	"strings"
	"testing"
)

func TestSDKKeyringReferenceAndLegacyReference(t *testing.T) {
	for _, prefix := range []string{"", "keyring:"} {
		c := Default()
		c.CredentialRef = prefix + strings.Repeat("a", 32)
		c.AccessKey = ""
		c.SecretKey = ""
		c.SessionToken = ""
		if err := c.ValidateOptions(); err != nil {
			t.Fatalf("%s reference: %v", prefix, err)
		}
	}
	for _, ref := range []string{"keyring:", "keyring:../secret", "keyring:" + strings.Repeat("a", 31), "keyring:keyring:" + strings.Repeat("a", 32)} {
		c := Default()
		c.CredentialRef = ref
		if err := c.ValidateOptions(); err == nil {
			t.Fatalf("accepted malformed reference %q", ref)
		}
	}
}
