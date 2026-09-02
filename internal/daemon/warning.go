package daemon

// SupervisionWarning is printed whenever a service is put under the OS
// supervisor. macOS applies CPU and I/O limits per resource coalition and
// places a launchd job in one of its own, so a supervised service competes
// with the compiler it serves instead of sharing its budget.
const SupervisionWarning = "⚠️  Supervised services are measurably slower than letting the build start them: " +
	"a launchd job is placed in its own resource coalition and competes with the compiler it serves, " +
	"and no plist setting closes that gap. See docs/daemon-latency.md. " +
	"Prefer the default, where the build starts the proxy and the ccache helper on demand."
