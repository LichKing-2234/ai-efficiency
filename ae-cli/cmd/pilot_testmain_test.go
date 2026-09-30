package cmd

import (
	"os"
	"testing"
)

// TestMain stops the whole package from touching LoongSuite Pilot.
//
// Without this, any test that reaches a command calling ensurePilot downloads
// and runs Pilot's real installer against the developer's own machine — which
// is exactly what happened the first time this wiring was added.
func TestMain(m *testing.M) {
	if err := os.Setenv(skipPilotEnv, "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
