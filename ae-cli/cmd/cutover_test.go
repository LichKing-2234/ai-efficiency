package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/spf13/cobra"
)

func TestRootCommandHasSessionlessPrimaryCommands(t *testing.T) {
	expected := map[string]bool{
		"init":   false,
		"sync":   false,
		"doctor": false,
	}

	for _, cmd := range rootCmd.Commands() {
		if _, ok := expected[cmd.Name()]; ok {
			expected[cmd.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Fatalf("expected subcommand %q not found", name)
		}
	}
}

func TestLegacyWorkflowCommandsAreHidden(t *testing.T) {
	legacy := []*cobraCommandRef{
		{name: "start", cmd: startCmd},
		{name: "stop", cmd: stopCmd},
		{name: "run", cmd: runCmd},
		{name: "attach", cmd: attachCmd},
		{name: "ps", cmd: psCmd},
		{name: "kill", cmd: killCmd},
		{name: "shell", cmd: shellCmd},
		{name: "flush", cmd: flushCmd},
	}

	for _, item := range legacy {
		if !item.cmd.Hidden {
			t.Fatalf("expected legacy command %q to be hidden", item.name)
		}
	}
}

func TestLegacyWorkflowCommandsReturnMigrationGuidance(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
	}{
		{name: "start", run: func() error { return startCmd.RunE(startCmd, nil) }},
		{name: "stop", run: func() error { return stopCmd.RunE(stopCmd, nil) }},
		{name: "run", run: func() error { return runCmd.RunE(runCmd, []string{"claude"}) }},
		{name: "attach", run: func() error { return attachCmd.RunE(attachCmd, nil) }},
		{name: "ps", run: func() error { return psCmd.RunE(psCmd, nil) }},
		{name: "kill", run: func() error { return killCmd.RunE(killCmd, []string{"%1"}) }},
		{name: "shell", run: func() error { return shellCmd.RunE(shellCmd, nil) }},
		{name: "flush", run: func() error { return flushCmd.RunE(flushCmd, nil) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil {
				t.Fatal("expected migration error")
			}
			msg := err.Error()
			for _, want := range []string{"legacy workflow", "ae-cli init", "ae-cli sync", "ae-cli doctor"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("error = %q, want substring %q", msg, want)
				}
			}
		})
	}
}

type cobraCommandRef struct {
	name string
	cmd  *cobra.Command
}

func TestInitCommandCreatesAttributionStateDir(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	origInstallSharedHooks := installSharedHooks
	installSharedHooks = func(cwd, selfPath string) error { return nil }
	t.Cleanup(func() { installSharedHooks = origInstallSharedHooks })

	buf := new(bytes.Buffer)
	initCmd.SetOut(buf)
	initCmd.SetErr(buf)

	if err := initCmd.RunE(initCmd, nil); err != nil {
		t.Fatalf("initCmd.RunE: %v", err)
	}

	if _, err := os.Stat(attributionlocal.AttributionRootDir()); err != nil {
		t.Fatalf("expected attribution root dir to exist, stat err=%v", err)
	}
	if !strings.Contains(buf.String(), "Initialized sessionless attribution.") {
		t.Fatalf("output = %q, want init success summary", buf.String())
	}
}

func TestDoctorCommandPrintsWorkspaceIdentity(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	buf := new(bytes.Buffer)
	doctorCmd.SetOut(buf)
	doctorCmd.SetErr(buf)

	if err := doctorCmd.RunE(doctorCmd, nil); err != nil {
		t.Fatalf("doctorCmd.RunE: %v", err)
	}

	output := buf.String()
	for _, want := range []string{"Sessionless attribution doctor", "Workspace ID:", "State Dir:"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want substring %q", output, want)
		}
	}
}

func TestSyncCommandRequiresLogin(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	oldCfg := cfg
	cfg = nil
	t.Cleanup(func() { cfg = oldCfg })

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	err = syncCmd.RunE(syncCmd, nil)
	if err == nil {
		t.Fatal("expected login requirement error")
	}
	if !strings.Contains(err.Error(), "ae-cli login") {
		t.Fatalf("err = %q, want login guidance", err.Error())
	}
}
