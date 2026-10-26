package gui

import "path/filepath"

// MyGo forwards os.Args[1:], without the executable. Relative files belong to
// the launching instance's working directory, not the already-running app's.
func secondInstanceFiles(args []string, workingDir string) []string {
	files := make([]string, 0, len(args))
	for _, argument := range args {
		if argument == "" {
			continue
		}
		if !filepath.IsAbs(argument) {
			argument = filepath.Join(workingDir, argument)
		}
		files = append(files, argument)
	}
	return files
}
