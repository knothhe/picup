package r2oauth

import (
	"bytes"
	"strings"
	"sync"

	"github.com/knothhe/r2connect/keyring"
)

// sessionSecrets retains successful bucket reads for this process's connection session.
// The SDK store still validates requests and stops access after a refusal; its
// native backend still owns every persistent credential operation.
type sessionSecrets struct {
	*keyring.Store
	reads *sessionSecretBackend
}

func newSessionSecrets(service string, backend keyring.Backend) *sessionSecrets {
	reads := &sessionSecretBackend{backend: backend, values: make(map[sessionSecretKey][]byte)}
	return &sessionSecrets{Store: keyring.NewWithBackend(service, reads), reads: reads}
}

// Forget erases retained bytes without accessing the native store or permitting
// another attempt after a refused prompt.
func (s *sessionSecrets) Forget() { s.reads.forget() }

// RetryAccess belongs only to an explicit user recovery or reconnect action.
func (s *sessionSecrets) RetryAccess() {
	s.Forget()
	s.Store.RetryAccess()
}

type sessionSecretKey struct{ service, key string }

type sessionSecretBackend struct {
	mu      sync.Mutex
	backend keyring.Backend
	values  map[sessionSecretKey][]byte
}

func (b *sessionSecretBackend) Load(service, key string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Bucket credentials have versioned keys, including imported legacy keys.
	// OAuth grants can rotate in place without a public-state change, so those
	// and unknown key types must always be read from the native store.
	if !strings.HasPrefix(key, "connection-") {
		return b.backend.Load(service, key)
	}
	id := sessionSecretKey{service, key}
	if value, ok := b.values[id]; ok {
		return bytes.Clone(value), nil
	}
	value, err := b.backend.Load(service, key)
	if err != nil || len(value) == 0 || len(value) > keyring.MaxValueBytes {
		// Let the SDK validate the result and clear any returned bytes.
		return value, err
	}
	b.values[id] = bytes.Clone(value)
	return value, nil
}

func (b *sessionSecretBackend) Save(service, key string, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.remove(sessionSecretKey{service, key})
	// Do not populate from writes: subsequent reads and migration verification
	// must reach the native store rather than trust the input to Save.
	return b.backend.Save(service, key, value)
}

func (b *sessionSecretBackend) Delete(service, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.remove(sessionSecretKey{service, key})
	return b.backend.Delete(service, key)
}

func (b *sessionSecretBackend) remove(id sessionSecretKey) {
	clear(b.values[id])
	delete(b.values, id)
}

func (b *sessionSecretBackend) forget() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id := range b.values {
		b.remove(id)
	}
}
