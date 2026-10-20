package picup

import (
	_ "embed"
	"encoding/json"
)

//go:embed mygo.json
var manifest []byte

// Version reads the same embedded manifest that MyGo uses to package the app.
func Version() string {
	var config struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(manifest, &config); err != nil || config.Version == "" {
		panic("mygo.json must contain a valid version")
	}
	return config.Version
}
