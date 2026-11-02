package selection

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestFinderOutputErrorsPreservePermissionCancellationAndDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, stderr, diagnostic string
		cancel                   bool
		want                     error
	}{
		{name: "permission", stderr: "not authorized (-1743)", want: ErrPermission},
		{name: "script error", stderr: "Finder scripting error (-1728)", diagnostic: "Finder scripting error (-1728)"},
		{name: "cancel", stderr: "killed", cancel: true, want: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err := readFinderOutput(ctx, func(context.Context) ([]byte, error) {
				if test.cancel {
					cancel()
				}
				return nil, &exec.ExitError{Stderr: []byte(test.stderr)}
			})
			if err == nil || test.want != nil && !errors.Is(err, test.want) || test.diagnostic != "" && !strings.Contains(err.Error(), test.diagnostic) {
				t.Fatal("Finder failure lost its error classification or detail", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readFinderOutput(ctx, func(context.Context) ([]byte, error) { return []byte("[]"), nil }); !errors.Is(err, context.Canceled) {
		t.Fatal("late successful output overrode cancellation", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 0)
	defer cancel()
	if _, err := readFinderOutput(ctx, func(context.Context) ([]byte, error) { return nil, errors.New("killed") }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("Finder timeout was reported as a generic process failure", err)
	}
}
