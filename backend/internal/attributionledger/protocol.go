package attributionledger

import (
	"fmt"
	"strings"
)

const (
	LedgerEpochShadowV2        = "shadow_v2"
	LedgerEpochFormalV2        = "formal_v2"
	V1WritePolicyAccept        = "accept"
	V1WritePolicyUpgradeNeeded = "upgrade_required"
)

type ProtocolContract struct {
	LedgerEpoch       string `json:"ledger_epoch"`
	V1WritePolicy     string `json:"v1_write_policy"`
	MinimumCLIVersion string `json:"minimum_cli_version,omitempty"`
}

func DefaultProtocolContract() ProtocolContract {
	return ProtocolContract{LedgerEpoch: LedgerEpochShadowV2, V1WritePolicy: V1WritePolicyAccept}
}

func NormalizeProtocolContract(contract ProtocolContract) (ProtocolContract, error) {
	contract.LedgerEpoch = strings.TrimSpace(contract.LedgerEpoch)
	contract.V1WritePolicy = strings.TrimSpace(contract.V1WritePolicy)
	contract.MinimumCLIVersion = strings.TrimSpace(contract.MinimumCLIVersion)
	if contract.LedgerEpoch == "" && contract.V1WritePolicy == "" && contract.MinimumCLIVersion == "" {
		return DefaultProtocolContract(), nil
	}
	switch {
	case contract.LedgerEpoch == LedgerEpochShadowV2 && contract.V1WritePolicy == V1WritePolicyAccept && contract.MinimumCLIVersion == "":
		return contract, nil
	case (contract.LedgerEpoch == LedgerEpochShadowV2 || contract.LedgerEpoch == LedgerEpochFormalV2) && contract.V1WritePolicy == V1WritePolicyUpgradeNeeded && contract.MinimumCLIVersion != "":
		return contract, nil
	default:
		return ProtocolContract{}, fmt.Errorf("invalid attribution protocol contract: ledger_epoch=%q v1_write_policy=%q minimum_cli_version=%q", contract.LedgerEpoch, contract.V1WritePolicy, contract.MinimumCLIVersion)
	}
}
