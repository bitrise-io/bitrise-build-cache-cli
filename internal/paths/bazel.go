package paths

import "path/filepath"

const (
	// BazelrcFileName is the per-user bazelrc filename under $HOME.
	BazelrcFileName = ".bazelrc"
)

// BazelrcFile returns the absolute path of the per-user ~/.bazelrc file.
func (p Paths) BazelrcFile() string {
	return filepath.Join(p.Home, BazelrcFileName)
}
