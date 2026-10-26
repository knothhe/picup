//go:build darwin && cgo

package r2oauth

// The SDK owns all native credential operations, including denial and chunks.
func systemStoreAvailable() bool { return true }
