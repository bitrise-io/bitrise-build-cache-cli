//go:build unit || integration

package trustedurl

// AllowLoopbackForTests lets a test binary point an endpoint env at an httptest server.
// It is compiled only with the unit or integration tag, never into the release binary.
func AllowLoopbackForTests() { allowLoopback = true }
