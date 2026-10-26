//go:build !darwin

package r2oauth

func systemStoreAvailable() bool { return false }

type unavailableStore struct{}

func newSystemStore(string) secretStore             { return unavailableStore{} }
func (unavailableStore) Get(string) ([]byte, error) { return nil, ErrSecretUnavailable }
func (unavailableStore) Set(string, []byte) error   { return ErrSecretUnavailable }
func (unavailableStore) Delete(string) error        { return ErrSecretUnavailable }
