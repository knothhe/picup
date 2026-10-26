//go:build !darwin || !cgo

package r2oauth

func systemStoreAvailable() bool { return false }
