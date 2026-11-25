package cmd

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/reporting"
	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
)

func stubSetup(t *testing.T, config *reporting.Config, providers []toolconfig.Provider, listErr, saveErr, hooksErr error) (*int, *int) {
	t.Helper()
	savedID := 0
	hookCalls := 0
	origList, origLoad, origSave, origHooks := setupListProviders, setupLoadReporting, setupSaveProviderID, setupEnableHooks
	setupListProviders = func(context.Context) ([]toolconfig.Provider, error) { return providers, listErr }
	setupLoadReporting = func() (*reporting.Config, error) { return config, nil }
	setupSaveProviderID = func(id int) error {
		savedID = id
		return saveErr
	}
	setupEnableHooks = func(string) error {
		hookCalls++
		return hooksErr
	}
	t.Cleanup(func() {
		setupListProviders, setupLoadReporting, setupSaveProviderID, setupEnableHooks = origList, origLoad, origSave, origHooks
	})
	return &savedID, &hookCalls
}

// Without a relay provider id the claim sync returns at its first guard without
// a message. Login is the one command every developer runs, so it is where the
// identifier has to be recorded.
func TestLoginSetupRecordsARelayProvider(t *testing.T) {
	saved, _ := stubSetup(t, &reporting.Config{}, []toolconfig.Provider{
		{ID: 3, Name: "other"},
		{ID: 7, Name: "sub2api", DisplayName: "Sub2API", IsPrimary: true},
	}, nil, nil, nil)

	var out, errOut bytes.Buffer
	if id := ensureRelayProvider(context.Background(), &out, &errOut); id != 7 {
		t.Fatalf("provider in effect = %d, want the one it just recorded", id)
	}

	if *saved != 7 {
		t.Fatalf("saved provider = %d, want the primary one (7)", *saved)
	}
	if !strings.Contains(out.String(), "Sub2API") {
		t.Fatalf("out = %q, want it to name the provider it chose", out.String())
	}
}

// A machine that already chose a provider through discover keeps that choice.
func TestLoginSetupLeavesAnExistingProviderAlone(t *testing.T) {
	saved, _ := stubSetup(t, &reporting.Config{RelayProviderID: 42}, []toolconfig.Provider{{ID: 7}}, nil, nil, nil)

	var out, errOut bytes.Buffer
	if id := ensureRelayProvider(context.Background(), &out, &errOut); id != 42 {
		t.Fatalf("provider in effect = %d, want the recorded choice reported back", id)
	}

	if *saved != 0 {
		t.Fatalf("saved provider = %d, want the existing choice untouched", *saved)
	}
	if out.Len() != 0 {
		t.Fatalf("out = %q, want silence when nothing changed", out.String())
	}
}

// Nothing here may fail a login, but nothing may fail quietly either: silence
// is what made the missing provider id so hard to notice.
func TestLoginSetupReportsEveryFailureWithoutFailingLogin(t *testing.T) {
	for _, tc := range []struct {
		name    string
		listErr error
		saveErr error
		want    string
	}{
		{name: "providers unreachable", listErr: fmt.Errorf("network down"), want: "ae-cli discover"},
		{name: "state unwritable", saveErr: fmt.Errorf("read-only"), want: "ae-cli discover"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubSetup(t, &reporting.Config{}, []toolconfig.Provider{{ID: 7, Name: "sub2api"}}, tc.listErr, tc.saveErr, nil)
			var out, errOut bytes.Buffer
			if id := ensureRelayProvider(context.Background(), &out, &errOut); id != 0 {
				t.Fatalf("provider in effect = %d, want zero so a caller cannot claim delivery is active", id)
			}
			if !strings.Contains(errOut.String(), tc.want) {
				t.Fatalf("errOut = %q, want it to name the recovery command", errOut.String())
			}
			if !strings.Contains(errOut.String(), "Commit attribution") {
				t.Fatalf("errOut = %q, want it to say what stops working", errOut.String())
			}
		})
	}
}

// Writing the user's global core.hooksPath reaches every repository on the
// machine, so it is announced rather than done quietly.
func TestLoginSetupEnablesManagedHooksAndSaysSo(t *testing.T) {
	_, hookCalls := stubSetup(t, &reporting.Config{}, nil, nil, nil, nil)
	var out, errOut bytes.Buffer
	ensureManagedHooks(&out, &errOut)

	if *hookCalls != 1 {
		t.Fatalf("hook installs = %d, want 1", *hookCalls)
	}
	if !strings.Contains(out.String(), "every repository") {
		t.Fatalf("out = %q, want it to say the change is machine-wide", out.String())
	}
}

func TestLoginSetupReportsAFailedHookInstall(t *testing.T) {
	stubSetup(t, &reporting.Config{}, nil, nil, nil, fmt.Errorf("hooksPath is owned by something else"))
	var out, errOut bytes.Buffer
	ensureManagedHooks(&out, &errOut)
	if !strings.Contains(errOut.String(), "hooks enable --global") {
		t.Fatalf("errOut = %q, want it to name the recovery command", errOut.String())
	}
}
