package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/ai-efficiency/ae-cli/internal/buildinfo"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/reporting"
	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
)

// Seams the tests replace so a login never reaches the network or the user's
// global Git configuration.
var (
	setupListProviders = func(ctx context.Context) ([]toolconfig.Provider, error) {
		items, err := defaultListProvidersForDiscover(ctx)
		if err != nil {
			return nil, err
		}
		return mapProviders(items), nil
	}
	setupLoadReporting  = func() (*reporting.Config, error) { return reporting.Load("") }
	setupSaveProviderID = preserveDiscoveredRelayProvider
	setupEnableHooks    = func(cwd string) error {
		return enableGlobalHooks(hooks.InstallOptions{
			CWD: cwd, NonInteractive: true, GeneratorVersion: buildinfo.Version,
		})
	}
)

// completeMachineSetup finishes everything a machine needs before its commits
// can be attributed.
//
// Login used to do only a third of it, and the remaining two thirds failed
// quietly. Without a relay provider id the claim sync returns at its first
// guard without a message, without a gap reason, and without anything doctor
// reports — usage keeps flowing while commit attribution silently produces
// nothing, which reads exactly like having written no attributable code.
// Without managed Git hooks nothing runs at commit time at all.
//
// Pointing at another command instead is what was tried: login already ended by
// suggesting `ae-cli discover`, and a suggestion is not a setup step. Anything
// required to make the product work belongs in the one command every developer
// runs.
//
// Nothing here fails the login. A machine that could not be fully set up is
// still logged in, and each step says what happened.
func completeMachineSetup(ctx context.Context, out, errOut io.Writer) {
	ensurePilot(ctx, out, errOut)
	ensureRelayProvider(ctx, out, errOut)
	ensureManagedHooks(out, errOut)
}

// ensureRelayProvider records which relay provider this machine reports under.
//
// It writes the identifier only. Choosing a provider also has a visible side of
// it — pointing each agent's base URL at the relay — and that stays in
// `discover`, where someone asked for it. The identifier is local state that
// nothing else can supply, and commit attribution is inert without it.
// It returns the provider in effect, or zero when the machine still has
// none — the caller may need to know before claiming delivery is active.
func ensureRelayProvider(ctx context.Context, out, errOut io.Writer) int {
	config, err := setupLoadReporting()
	if err != nil {
		fmt.Fprintf(errOut, "Warning: could not read reporting state (%v). Commit attribution stays off until 'ae-cli discover' runs.\n", err)
		return 0
	}
	// Reading rather than creating. Switching accounts deletes this state on
	// purpose, and recording a provider into a file that should not exist would
	// resurrect the credentials the switch just cleared.
	if config == nil {
		return 0
	}
	if config.RelayProviderID > 0 {
		return config.RelayProviderID
	}

	providers, err := setupListProviders(ctx)
	if err != nil {
		fmt.Fprintf(errOut, "Warning: could not list relay providers (%v). Commit attribution stays off until 'ae-cli discover' runs.\n", err)
		return 0
	}
	provider, err := toolconfig.SelectProvider(providers, "")
	if err != nil {
		fmt.Fprintf(errOut, "Warning: no relay provider available (%v). Commit attribution stays off until one is configured.\n", err)
		return 0
	}
	if err := setupSaveProviderID(provider.ID); err != nil {
		fmt.Fprintf(errOut, "Warning: could not record the relay provider (%v). Commit attribution stays off until 'ae-cli discover' runs.\n", err)
		return 0
	}
	fmt.Fprintf(out, "Reporting under relay provider %s.\n", providerLabel(provider))
	return provider.ID
}

func providerLabel(provider toolconfig.Provider) string {
	if provider.DisplayName != "" {
		return provider.DisplayName
	}
	if provider.Name != "" {
		return provider.Name
	}
	return fmt.Sprintf("#%d", provider.ID)
}

// ensureManagedHooks installs the Git hooks that start a scan after a commit.
//
// This writes the user's global core.hooksPath, which reaches every repository
// on the machine, so it says so rather than doing it quietly.
func ensureManagedHooks(out, errOut io.Writer) {
	cwd, _ := os.Getwd()
	if err := setupEnableHooks(cwd); err != nil {
		fmt.Fprintf(errOut, "Warning: could not enable managed Git hooks (%v). Commits will not be attributed until 'ae-cli hooks enable --global' runs.\n", err)
		return
	}
	fmt.Fprintln(out, "Managed Git hooks enabled for every repository on this machine.")
}
