//go:build unit

package enrichment_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
)

func TestHealthWriter_UpdateAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "health.json")
	hw := &enrichment.HealthWriter{Path: path}

	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	require.NoError(t, hw.Update(func(s *enrichment.HealthSnapshot) {
		s.LastAttempt = now
		s.LastSuccess = now
	}))

	snap, err := enrichment.LoadHealth(path)
	require.NoError(t, err)
	assert.Equal(t, now, snap.LastAttempt.UTC())
	assert.Equal(t, now, snap.LastSuccess.UTC())
	assert.Zero(t, snap.ConsecutiveErrors)
}

func TestHealthWriter_MultipleUpdatesAccumulate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "health.json")
	hw := &enrichment.HealthWriter{Path: path}

	base := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)

	require.NoError(t, hw.Update(func(s *enrichment.HealthSnapshot) {
		s.LastAttempt = base
		s.LastError = "boom"
		s.LastErrorAt = base
		s.ConsecutiveErrors = 1
	}))

	require.NoError(t, hw.Update(func(s *enrichment.HealthSnapshot) {
		s.LastAttempt = base.Add(time.Minute)
		s.LastError = "still boom"
		s.LastErrorAt = base.Add(time.Minute)
		s.ConsecutiveErrors++
	}))

	snap, err := enrichment.LoadHealth(path)
	require.NoError(t, err)
	assert.Equal(t, base.Add(time.Minute), snap.LastAttempt.UTC())
	assert.Equal(t, 2, snap.ConsecutiveErrors)
	assert.Equal(t, "still boom", snap.LastError)
}

func TestLoadHealth_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := enrichment.LoadHealth(filepath.Join(dir, "missing.json"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist), "LoadHealth must surface os.ErrNotExist unchanged")
}

func TestTickSuccess_MatchedFalseLeavesLastMatchedZero(t *testing.T) {
	dir := t.TempDir()
	hw := &enrichment.HealthWriter{Path: filepath.Join(dir, "health.json")}

	now := time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)
	enrichment.TickSuccess(hw, nil, now, false)

	snap, err := enrichment.LoadHealth(hw.Path)
	require.NoError(t, err)
	assert.Equal(t, now, snap.LastSuccess.UTC(), "TickSuccess must bump LastSuccess")
	assert.True(t, snap.LastMatched.IsZero(), "matched=false must leave LastMatched untouched")
}

func TestTickSuccess_MatchedTrueBumpsBoth(t *testing.T) {
	dir := t.TempDir()
	hw := &enrichment.HealthWriter{Path: filepath.Join(dir, "health.json")}

	now := time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)
	enrichment.TickSuccess(hw, nil, now, true)

	snap, err := enrichment.LoadHealth(hw.Path)
	require.NoError(t, err)
	assert.Equal(t, now, snap.LastSuccess.UTC())
	assert.Equal(t, now, snap.LastMatched.UTC())
}

func TestTickSuccess_NilWriterIsNoop(t *testing.T) {
	assert.NotPanics(t, func() {
		enrichment.TickSuccess(nil, nil, time.Now(), true)
	})
}

func TestTickFailure_IncrementsConsecutiveErrors(t *testing.T) {
	dir := t.TempDir()
	hw := &enrichment.HealthWriter{Path: filepath.Join(dir, "health.json")}

	base := time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)

	enrichment.TickFailure(hw, nil, base, errors.New("boom-1"))
	enrichment.TickFailure(hw, nil, base.Add(time.Minute), errors.New("boom-2"))

	snap, err := enrichment.LoadHealth(hw.Path)
	require.NoError(t, err)
	assert.Equal(t, 2, snap.ConsecutiveErrors)
	assert.Equal(t, "boom-2", snap.LastError)
	assert.Equal(t, base.Add(time.Minute), snap.LastErrorAt.UTC())
}
