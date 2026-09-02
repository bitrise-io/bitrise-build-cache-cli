// Package spawn starts the cache services on demand and probes their sockets.
//
// The services are deliberately not supervised: macOS applies CPU and I/O
// limits per resource coalition, and a launchd job forms its own, so a
// supervised proxy competes with the compiler it serves and loses. Forking it
// from whoever needs it puts it in the build's coalition instead. See
// docs/daemon-latency.md.
package spawn

// Service is a cache service that is started on demand.
type Service struct {
	Name string
	Args []string
}

const (
	NameXcelerateProxy = "xcelerate-proxy"
	NameCcacheHelper   = "ccache-helper"
)

func XcelerateProxy() Service {
	return Service{Name: NameXcelerateProxy, Args: []string{"xcelerate", "start-proxy"}}
}

func CcacheHelper() Service {
	return Service{Name: NameCcacheHelper, Args: []string{"ccache", "storage-helper", "start"}}
}

// WithDebug prepends the persistent root flag, which has to precede the subcommand.
func (s Service) WithDebug() Service {
	s.Args = append([]string{"--debug"}, s.Args...)

	return s
}

func (s Service) WithArgs(extra ...string) Service {
	s.Args = append(append([]string{}, s.Args...), extra...)

	return s
}
