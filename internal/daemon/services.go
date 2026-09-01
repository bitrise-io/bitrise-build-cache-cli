package daemon

const LabelPrefix = "io.bitrise.build-cache."

const UnitPrefix = "bitrise-build-cache-"

const (
	ServiceXcelerateProxy = "xcelerate-proxy"
	ServiceCcacheHelper   = "ccache-helper"
)

type Service struct {
	Name string
	Args []string
}

func (s Service) Label() string {
	return LabelPrefix + s.Name
}

func (s Service) UnitName() string {
	return UnitPrefix + s.Name
}

// WithDebugLogging makes the supervised services log at debug level. A service
// manager starts them from the plist or unit file, so a --debug passed to
// activate never reaches them: without this the daemon's own diagnostics cannot
// be turned on at all.
func WithDebugLogging(services []Service) []Service {
	out := make([]Service, 0, len(services))
	for _, svc := range services {
		// --debug is a persistent root flag, so it has to precede the
		// subcommand.
		svc.Args = append([]string{"--debug"}, svc.Args...)
		out = append(out, svc)
	}

	return out
}

func DefaultServices() []Service {
	return []Service{
		{
			Name: ServiceXcelerateProxy,
			Args: []string{"xcelerate", "start-proxy"},
		},
		{
			Name: ServiceCcacheHelper,
			Args: []string{"ccache", "storage-helper", "start"},
		},
	}
}
