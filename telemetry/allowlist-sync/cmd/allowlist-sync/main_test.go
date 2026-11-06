package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// A value that does not parse must stop the sync: DRY_RUN=yes falling back to
// false would write.
func TestGetenvRejectsValuesThatDoNotParse(t *testing.T) {
	envErrs = nil
	t.Setenv("DRY_RUN", "yes")
	t.Setenv("INTERVAL", "60")
	getenvBool("DRY_RUN", false)
	getenvDuration("INTERVAL", time.Minute)
	assert.Len(t, envErrs, 2)

	envErrs = nil
	t.Setenv("DRY_RUN", "true")
	t.Setenv("INTERVAL", "")
	assert.True(t, getenvBool("DRY_RUN", false))
	assert.Equal(t, time.Minute, getenvDuration("INTERVAL", time.Minute))
	assert.Empty(t, envErrs)
}
