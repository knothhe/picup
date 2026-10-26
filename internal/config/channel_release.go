//go:build !picup_dev

package config

// BuildChannel and DirectoryName are fixed together at compilation. Formal
// desktop packages and their bundled CLI use the same untagged identity.
const (
	BuildChannel  = "release"
	DirectoryName = "picup"
)
