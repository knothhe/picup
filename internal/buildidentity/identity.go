// Package buildidentity gives installers and the desktop runtime one channel identity.
package buildidentity

import "github.com/knothhe/picup/internal/config"

const ReleaseIdentifier = "com.knothhe.picup"
const DevelopmentIdentifier = "com.knothhe.picup.dev"

func Development() bool { return config.BuildChannel == "development" }
func Name() string {
	if Development() {
		return "Picup Dev"
	}
	return "Picup"
}
func Identifier() string {
	if Development() {
		return DevelopmentIdentifier
	}
	return ReleaseIdentifier
}
func CLIName() string {
	if Development() {
		return "picup-dev"
	}
	return "picup"
}
