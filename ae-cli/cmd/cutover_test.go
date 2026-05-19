package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
)

func TestRootCommandHasSessionlessPrimaryCommands(t *testing.T) {
	expected := map[string]bool{
		"discover": false,
		"init":     false,
		"sync":     false,
		"doctor":   false,
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

func TestRootCommandDoesNotExposeLegacyWorkflowCommands(t *testing.T) {
	legacy := map[string]struct{}{
		"start":  {},
		"stop":   {},
		"run":    {},
		"attach": {},
		"ps":     {},
		"kill":   {},
		"shell":  {},
		"flush":  {},
	}

	for _, cmd := range rootCmd.Commands() {
		if _, ok := legacy[cmd.Name()]; ok {
			t.Fatalf("unexpected legacy command %q still registered", cmd.Name())
		}
	}
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
