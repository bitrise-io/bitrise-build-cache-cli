package proxy

import "time"

// TouchSession exposes the private touchSession helper for tests.
func (p *Proxy) TouchSession() { p.touchSession() }

// LastActivity exposes p.lastActivity for tests.
func (p *Proxy) LastActivity() time.Time {
	p.sessionMutex.Lock()
	defer p.sessionMutex.Unlock()

	return p.lastActivity
}

// SetLastActivity forces p.lastActivity for tests.
func (p *Proxy) SetLastActivity(t time.Time) {
	p.sessionMutex.Lock()
	defer p.sessionMutex.Unlock()

	p.lastActivity = t
}

// InactivityDuration exposes p.inactivityDuration for tests.
func (p *Proxy) InactivityDuration() time.Duration { return p.inactivityDuration() }

// IsSessionServiceMethod exposes the private helper for tests.
func IsSessionServiceMethod(fullMethod string) bool { return isSessionServiceMethod(fullMethod) }
