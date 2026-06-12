package daemon

// LabelPrefix is the reverse-DNS prefix used for every LaunchAgent label this
// CLI registers. Keeping a single prefix makes wholesale uninstall / status
// queries straightforward (`launchctl list | grep io.bitrise.build-cache.`).
const LabelPrefix = "io.bitrise.build-cache."

// UnitPrefix is the systemd unit name prefix on Linux. systemd unit names
// don't follow reverse-DNS; we use a dash-separated form so `systemctl --user
// list-units 'bitrise-build-cache-*'` groups them.
const UnitPrefix = "bitrise-build-cache-"

// Service describes one supervised long-lived process the daemon installs.
// The Args slice is appended to the CLI executable path to form the supervisor
// program-arguments / ExecStart line.
type Service struct {
	// Name is a short identifier (xcelerate-proxy, ccache-helper). Used for log
	// filenames, launchd label, and systemd unit name.
	Name string
	// Args is the argv passed to the CLI binary, e.g. ["xcelerate", "start-proxy"].
	Args []string
}

// Label returns the launchd Label this service is registered under.
func (s Service) Label() string {
	return LabelPrefix + s.Name
}

// UnitName returns the bare systemd unit name (without the `.service` suffix).
// Pass to `systemctl --user enable --now <unit-name>`.
func (s Service) UnitName() string {
	return UnitPrefix + s.Name
}

// DefaultServices returns the canonical set of services the daemon supervises.
// Order matters only for human-readable output; the OS supervisor starts them
// independently.
func DefaultServices() []Service {
	return []Service{
		{
			Name: "xcelerate-proxy",
			Args: []string{"xcelerate", "start-proxy"},
		},
		{
			Name: "ccache-helper",
			Args: []string{"ccache", "storage-helper", "start"},
		},
	}
}
