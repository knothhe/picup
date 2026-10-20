package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHomePathDisplayAndExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ absolute, display string }{
		{home, "~"},
		{filepath.Join(home, ".local", "bin"), filepath.Join("~", ".local", "bin")},
		{home + "-other", home + "-other"},
		{filepath.Join(home+"-other", "bin"), filepath.Join(home+"-other", "bin")},
	} {
		if got := DisplayPath(test.absolute); got != test.display {
			t.Fatalf("display path = %q, want %q", got, test.display)
		}
		got, err := expandHome(test.display)
		if err != nil || got != test.absolute {
			t.Fatalf("expanded path = %q, want %q: %v", got, test.absolute, err)
		}
		if OnPath(test.display) != OnPath(test.absolute) {
			t.Fatal("abbreviating the home directory changed PATH detection")
		}
	}
}
