package r2oauth

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/knothhe/r2connect/keyring"
)

type sessionBackendFixture struct {
	mu                          sync.Mutex
	values                      map[sessionSecretKey][]byte
	loads, saves, deletes       int
	loadErr, saveErr, deleteErr error
}

func (b *sessionBackendFixture) Load(service, key string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.loads++
	return bytes.Clone(b.values[sessionSecretKey{service, key}]), b.loadErr
}

func (b *sessionBackendFixture) Save(service, key string, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.saves++
	if b.saveErr == nil {
		b.values[sessionSecretKey{service, key}] = bytes.Clone(value)
	}
	return b.saveErr
}

func (b *sessionBackendFixture) Delete(service, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.deletes++
	if b.deleteErr == nil {
		delete(b.values, sessionSecretKey{service, key})
	}
	return b.deleteErr
}

func newSessionFixture() (*sessionSecrets, *sessionBackendFixture) {
	backend := &sessionBackendFixture{values: map[sessionSecretKey][]byte{
		{"fixture", "connection-fixture"}: []byte("saved credential"),
	}}
	return newSessionSecrets("fixture", backend), backend
}

func TestSecretSessionConcurrentReadsAccessNativeStoreOnce(t *testing.T) {
	store, backend := newSessionFixture()
	var workers sync.WaitGroup
	for range 32 {
		workers.Go(func() {
			value, err := store.Load("connection-fixture")
			if err != nil || string(value) != "saved credential" {
				t.Errorf("load=%q %v", value, err)
			}
			clear(value)
		})
	}
	workers.Wait()
	if backend.loads != 1 {
		t.Fatalf("native reads=%d", backend.loads)
	}
	value, err := store.Load("connection-fixture")
	if err != nil || string(value) != "saved credential" {
		t.Fatalf("caller cleared cached bytes: %q %v", value, err)
	}
}

func TestSecretSessionDoesNotCacheErrorsOrInvalidValues(t *testing.T) {
	for _, test := range []struct {
		name  string
		value []byte
		err   error
	}{
		{"empty", nil, nil},
		{"too large", make([]byte, keyring.MaxValueBytes+1), nil},
		{"error with data", []byte("unusable"), errors.New("fixture read failure")},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, backend := newSessionFixture()
			backend.values[sessionSecretKey{"fixture", "connection-fixture"}] = test.value
			backend.loadErr = test.err
			for range 2 {
				if value, err := store.Load("connection-fixture"); err == nil || value != nil {
					t.Fatalf("invalid read=%q %v", value, err)
				}
			}
			if backend.loads != 2 || len(store.reads.values) != 0 {
				t.Fatalf("invalid data was cached: reads=%d cached=%d", backend.loads, len(store.reads.values))
			}
		})
	}
}

func TestSecretSessionCachesOnlyVersionedBucketCredentials(t *testing.T) {
	for _, key := range []string{"oauth-fixture", "oauth-legacy-fixture", "future-secret-type"} {
		t.Run(key, func(t *testing.T) {
			store, backend := newSessionFixture()
			id := sessionSecretKey{"fixture", key}
			backend.values[id] = []byte("first value")
			if _, err := store.Load(key); err != nil {
				t.Fatal(err)
			}
			backend.values[id] = []byte("updated by another process")
			value, err := store.Load(key)
			if err != nil || string(value) != "updated by another process" || backend.loads != 2 {
				t.Fatalf("mutable or unknown credential was cached: %v reads=%d", err, backend.loads)
			}
			clear(value)
			if len(store.reads.values) != 0 {
				t.Fatal("non-bucket credential remained in the read cache")
			}
		})
	}
}

func TestSecretSessionSaveRequiresNativeReadback(t *testing.T) {
	store, backend := newSessionFixture()
	if _, err := store.Load("connection-fixture"); err != nil {
		t.Fatal(err)
	}
	old := store.reads.values[sessionSecretKey{"fixture", "connection-fixture"}]
	if err := store.Save("connection-fixture", []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(old, make([]byte, len(old))) || len(store.reads.values) != 0 {
		t.Fatal("save retained the previous read or cached its unverified input")
	}
	// A write can appear successful while subsequent native access fails. The
	// adapter must preserve the SDK's opportunity to detect that failure.
	backend.loadErr = errors.New("fixture readback failure")
	if _, err := store.Load("connection-fixture"); !errors.Is(err, backend.loadErr) {
		t.Fatalf("readback=%v", err)
	}
	if backend.loads != 2 || backend.saves != 1 {
		t.Fatalf("native reads=%d writes=%d", backend.loads, backend.saves)
	}
}

func TestSecretSessionFailedMutationInvalidatesPreviousRead(t *testing.T) {
	for _, operation := range []string{"save", "delete"} {
		t.Run(operation, func(t *testing.T) {
			store, backend := newSessionFixture()
			if _, err := store.Load("connection-fixture"); err != nil {
				t.Fatal(err)
			}
			failure := errors.New("fixture mutation failure")
			var err error
			if operation == "save" {
				backend.saveErr = failure
				err = store.Save("connection-fixture", []byte("replacement"))
			} else {
				backend.deleteErr = failure
				err = store.Delete("connection-fixture")
			}
			if !errors.Is(err, failure) || len(store.reads.values) != 0 {
				t.Fatalf("mutation=%v cached=%d", err, len(store.reads.values))
			}
			if _, err := store.Load("connection-fixture"); err != nil || backend.loads != 2 {
				t.Fatalf("next read did not access native store: %v reads=%d", err, backend.loads)
			}
		})
	}
}

func TestSecretSessionForgetErasesBytesAndSeparatesServices(t *testing.T) {
	store, backend := newSessionFixture()
	backend.values[sessionSecretKey{"another", "connection-fixture"}] = []byte("another credential")
	for service, want := range map[string]string{"fixture": "saved credential", "another": "another credential"} {
		value, err := store.reads.Load(service, "connection-fixture")
		if err != nil || string(value) != want {
			t.Fatalf("service %q read=%q %v", service, value, err)
		}
	}
	retained := make([][]byte, 0, len(store.reads.values))
	for _, value := range store.reads.values {
		retained = append(retained, value)
	}
	store.Forget()
	for _, value := range retained {
		if !bytes.Equal(value, make([]byte, len(value))) {
			t.Fatal("forgotten credential bytes were not erased")
		}
	}
	if len(store.reads.values) != 0 {
		t.Fatal("forgotten credentials remained cached")
	}
	if _, err := store.Load("connection-fixture"); err != nil || backend.loads != 3 {
		t.Fatalf("post-forget read=%v reads=%d", err, backend.loads)
	}
}

func TestSecretSessionRetryClearsReadsAndPreservesSDKDenialLatch(t *testing.T) {
	store, backend := newSessionFixture()
	if _, err := store.Load("connection-fixture"); err != nil {
		t.Fatal(err)
	}
	backend.loadErr = keyring.ErrAccessDenied
	if _, err := store.Load("other"); !errors.Is(err, keyring.ErrAccessDenied) {
		t.Fatalf("refusal=%v", err)
	}
	backend.loadErr = nil
	if _, err := store.Load("connection-fixture"); !errors.Is(err, keyring.ErrAccessDenied) {
		t.Fatalf("cached read bypassed SDK denial: %v", err)
	}
	if backend.loads != 2 {
		t.Fatal("refused store accessed native backend again")
	}
	store.RetryAccess()
	if _, err := store.Load("connection-fixture"); err != nil || backend.loads != 3 {
		t.Fatalf("retry did not clear cached read and refusal: %v reads=%d", err, backend.loads)
	}
}
