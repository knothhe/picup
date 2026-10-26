package picup

import "encoding/json"

const (
	RepositoryURL = "https://github.com/knothhe/picup"
	IssuesURL     = RepositoryURL + "/issues"
	ReleasesURL   = RepositoryURL + "/releases"
)

// UpdateSettings contains only the public updater trust and feed configuration.
// Signing secrets and local publisher paths are never exposed to the app.
type UpdateSettings struct {
	PublicKey string `json:"publicKey"`
	GitHub    string `json:"github"`
	TagPrefix string `json:"tagPrefix"`
	URL       string `json:"url"`
}

// UpdateConfig shares the embedded manifest used by Version and MyGo packaging.
// A missing updates section deliberately leaves update installation unavailable.
func UpdateConfig() UpdateSettings {
	var config struct {
		Updates UpdateSettings `json:"updates"`
	}
	if json.Unmarshal(manifest, &config) != nil {
		return UpdateSettings{}
	}
	return config.Updates
}
