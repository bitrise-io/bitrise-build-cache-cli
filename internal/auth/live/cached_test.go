//go:build unit

package live

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth/store"
)

func cachedWithBroker(broker func(context.Context, map[string]string) (auth.Credential, error), now *time.Time) *Cached {
	r := &Resolver{Broker: broker, Backends: []store.Store{}, AnalyticsBlock: func() (auth.Credential, auth.Origin, bool) {
		return auth.Credential{}, auth.Origin{}, false
	}}
	c := r.Bind(map[string]string{}).Cached(time.Minute)
	c.now = func() time.Time { return *now }

	return c
}

func TestCached_Get(t *testing.T) {
	t.Run("reuses within the ttl, re-resolves after", func(t *testing.T) {
		now := time.Now()
		calls := 0
		c := cachedWithBroker(func(context.Context, map[string]string) (auth.Credential, error) {
			calls++

			return auth.Credential{Token: "jwt", WorkspaceID: "ws", Expiry: now.Add(time.Hour)}, nil
		}, &now)

		c.Get(context.Background())
		now = now.Add(30 * time.Second)
		c.Get(context.Background())
		assert.Equal(t, 1, calls)

		now = now.Add(time.Minute)
		c.Get(context.Background())
		assert.Equal(t, 2, calls)
	})

	t.Run("re-resolves inside the ttl when the credential is about to expire", func(t *testing.T) {
		now := time.Now()
		calls := 0
		c := cachedWithBroker(func(context.Context, map[string]string) (auth.Credential, error) {
			calls++

			return auth.Credential{Token: "jwt", WorkspaceID: "ws", Expiry: now.Add(90 * time.Second)}, nil
		}, &now)

		c.Get(context.Background())
		now = now.Add(40 * time.Second)
		c.Get(context.Background())
		assert.Equal(t, 2, calls)
	})

	t.Run("serves the last good credential when a re-resolve fails", func(t *testing.T) {
		now := time.Now()
		fail := false
		c := cachedWithBroker(func(context.Context, map[string]string) (auth.Credential, error) {
			if fail {
				return auth.Credential{}, errors.New("offline")
			}

			return auth.Credential{Token: "jwt", WorkspaceID: "ws", Expiry: now.Add(time.Hour)}, nil
		}, &now)

		first := c.Get(context.Background())
		fail = true
		now = now.Add(2 * time.Minute)
		assert.Equal(t, first, c.Get(context.Background()))
	})

	t.Run("serves the current credential while another caller re-resolves", func(t *testing.T) {
		now := time.Now()
		first := true
		started, release := make(chan struct{}), make(chan struct{})
		c := cachedWithBroker(func(context.Context, map[string]string) (auth.Credential, error) {
			if first {
				first = false

				return auth.Credential{Token: "old", WorkspaceID: "ws", Expiry: now.Add(time.Hour)}, nil
			}
			close(started)
			<-release

			return auth.Credential{Token: "new", WorkspaceID: "ws", Expiry: now.Add(time.Hour)}, nil
		}, &now)

		c.Get(context.Background())
		now = now.Add(2 * time.Minute)

		leader := make(chan auth.Credential)
		go func() { leader <- c.Get(context.Background()) }()
		<-started

		assert.Equal(t, "old", c.Get(context.Background()).Token)
		close(release)
		assert.Equal(t, "new", (<-leader).Token)
		assert.Equal(t, "new", c.Get(context.Background()).Token)
	})
}
