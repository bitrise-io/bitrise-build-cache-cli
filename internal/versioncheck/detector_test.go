//go:build unit

package versioncheck

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetect_firstRunWhenStateEmpty(t *testing.T) {
	res := Detect(State{}, "2.8.4")
	assert.Equal(t, FirstRun, res.Kind)
	assert.Empty(t, res.PreviousVersion)
	assert.Equal(t, "2.8.4", res.CurrentVersion)
}

func TestDetect_noChangeWhenVersionsMatch(t *testing.T) {
	res := Detect(State{LastVersion: "2.8.4"}, "2.8.4")
	assert.Equal(t, NoChange, res.Kind)
	assert.Equal(t, "2.8.4", res.PreviousVersion)
}

func TestDetect_bumpOnVersionChange(t *testing.T) {
	res := Detect(State{LastVersion: "2.8.3"}, "2.8.4")
	assert.Equal(t, Bump, res.Kind)
	assert.Equal(t, "2.8.3", res.PreviousVersion)
	assert.Equal(t, "2.8.4", res.CurrentVersion)
}

func TestDetect_bumpAlsoOnDowngrade(t *testing.T) {
	// Treating any non-equal version as a bump matches the design — D3
	// config refresh should run after a rollback too.
	res := Detect(State{LastVersion: "2.9.0"}, "2.8.4")
	assert.Equal(t, Bump, res.Kind)
}
