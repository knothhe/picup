package devmaintenance

import (
	"errors"
	"testing"
)

func TestLoginCleanupStatusAndPermissionErrors(t *testing.T) {
	for _, initial := range []int{0, 3} {
		if err := clearLogin(func() (int, error) { return initial, nil }, func() error { t.Fatal("unregistered absent login item"); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	for _, initial := range []int{1, 2} {
		status := initial
		if err := clearLogin(func() (int, error) { return status, nil }, func() error { status = 0; return nil }); err != nil {
			t.Fatal(err)
		}
		denied := errors.New("permission denied")
		if err := clearLogin(func() (int, error) { return initial, nil }, func() error { return denied }); !errors.Is(err, denied) {
			t.Fatal("swallowed real permission error", err)
		}
		if err := clearLogin(func() (int, error) { return initial, nil }, func() error { return nil }); err == nil {
			t.Fatal("claimed success while still registered")
		}
	}
	if err := clearLogin(func() (int, error) { return 42, nil }, func() error { t.Fatal("unknown status changed system state"); return nil }); err == nil {
		t.Fatal("accepted unknown status")
	}
}
