package cmd

import "testing"

func TestSyncStatusCommandIsRegistered(t *testing.T) {
	var found bool
	for _, c := range syncCmd.Commands() {
		if c.Name() == "status" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected sync status subcommand")
	}
}
