package proxy

// TouchSession exposes the private touchSession helper for tests.
func (p *Proxy) TouchSession() { p.touchSession() }
