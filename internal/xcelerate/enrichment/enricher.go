package enrichment

import (
	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/google/uuid"

	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/config/common"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/analytics"
)

//go:generate moq -stub -out invocation_putter_mock_test.go -pkg enrichment_test . InvocationPutter

type InvocationPutter interface {
	PutInvocation(inv analytics.Invocation) error
}

// Xcode.app never calls SetSession so no pending match; mint a fresh id.
type Enricher struct {
	Store        *Store
	Client       InvocationPutter
	Auth         configcommon.CacheAuthConfig
	Metadata     configcommon.CacheConfigMetadata
	XcodeVersion string
	Logger       log.Logger
}

func (e *Enricher) Enrich(entry ManifestEntry) {
	var (
		pending []PendingRecord
		err     error
	)

	if e.Store != nil {
		pending, err = e.Store.Load()
		if err != nil {
			e.logf("enrichment load pending: %s", err)
		}
	}

	invocationID, matched := Correlate(entry, pending)
	if !matched {
		invocationID = uuid.NewString()
	}

	inv := analytics.NewInvocation(analytics.InvocationRunStats{
		InvocationDate: entry.Start,
		InvocationID:   invocationID,
		Duration:       entry.Stop.Sub(entry.Start).Milliseconds(),
		Command:        string(entry.Command()),
		FullCommand:    entry.Signature,
		Success:        entry.Success(),
		XcodeVersion:   e.XcodeVersion,
	}, e.Auth, e.Metadata)

	if scheme := entry.SchemeName; scheme != "" {
		inv.Command = string(entry.Command()) + " " + scheme
	}

	if err := e.Client.PutInvocation(*inv); err != nil {
		e.logf("enrichment PutInvocation %s: %s", invocationID, err)

		return
	}

	e.logf("enrichment PUT %s (matched=%t scheme=%s cmd=%s)", invocationID, matched, entry.SchemeName, entry.Command())

	if matched && e.Store != nil {
		if err := e.Store.Remove(invocationID); err != nil {
			e.logf("enrichment remove pending %s: %s", invocationID, err)
		}
	}
}

func (e *Enricher) logf(format string, args ...any) {
	if e.Logger == nil {
		return
	}

	e.Logger.Debugf(format, args...)
}
