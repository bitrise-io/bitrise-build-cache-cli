package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

const LogDirRelative = ".local/state/bitrise-build-cache/logs"

const LaunchAgentsDirRelative = "Library/LaunchAgents"

const SystemdUserDirRelative = ".config/systemd/user"

type Paths struct {
	Home string
}

func NewPathsFromHome(home string) Paths {
	return Paths{Home: home}
}

func NewPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve user home dir: %w", err)
	}

	return Paths{Home: home}, nil
}

func (p Paths) LaunchAgentsDir() string {
	return filepath.Join(p.Home, LaunchAgentsDirRelative)
}

func (p Paths) SystemdUserDir() string {
	return filepath.Join(p.Home, SystemdUserDirRelative)
}

func (p Paths) LogDir() string {
	return filepath.Join(p.Home, LogDirRelative)
}

func (p Paths) PlistPath(label string) string {
	return filepath.Join(p.LaunchAgentsDir(), label+".plist")
}

func (p Paths) UnitPath(unitName string) string {
	return filepath.Join(p.SystemdUserDir(), unitName+".service")
}

func (p Paths) StdoutPath(service string) string {
	return filepath.Join(p.LogDir(), service+".out.log")
}

func (p Paths) StderrPath(service string) string {
	return filepath.Join(p.LogDir(), service+".err.log")
}
