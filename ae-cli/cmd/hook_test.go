package cmd

import "testing"

func TestHookCommandHasPostRewriteSubcommand(t *testing.T) {
	var found bool
	for _, c := range hookCmd.Commands() {
		if c.Name() == "post-rewrite" {
			found = true
			if !c.Hidden {
				t.Fatalf("expected hook post-rewrite to be hidden")
			}
		}
	}
	if !found {
		t.Fatalf("expected hidden subcommand 'ae-cli hook post-rewrite' to exist")
	}
}

