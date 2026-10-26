//go:build picup_dev

package config

// Development binaries keep preferences and credential namespaces separate
// even when they retain a formal-looking version or package identifier.
const (
	BuildChannel  = "development"
	DirectoryName = "picup-dev"
)
