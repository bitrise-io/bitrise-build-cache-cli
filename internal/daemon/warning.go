package daemon

// SupervisionWarning is printed whenever a service is put under the OS
// supervisor. macOS applies CPU and I/O limits per resource coalition and
// places a launchd job in one of its own, so a supervised service competes
// with the compiler it serves instead of sharing its budget.
const SupervisionWarning = "⚠️  Supervised services are measurably slower: on a 4-core machine the " +
	"launchd-hosted proxy served cache operations at 2314ms against 6.3ms for the same binary started " +
	"by the build. No plist setting closes that gap — see docs/daemon-latency.md. " +
	"Prefer the default, where the build starts the proxy and the ccache helper on demand."
