package teamaccount

import (
	"github.com/ai-efficiency/backend/ent/predicate"
	entrelayprovider "github.com/ai-efficiency/backend/ent/relayprovider"
	entteamaccount "github.com/ai-efficiency/backend/ent/teamaccount"
	entteamaccountevent "github.com/ai-efficiency/backend/ent/teamaccountevent"
	entteamaccountkey "github.com/ai-efficiency/backend/ent/teamaccountkey"
	entteamaccountmanager "github.com/ai-efficiency/backend/ent/teamaccountmanager"
)

func teamaccountDepartmentEQ(departmentID string) predicate.TeamAccount {
	return entteamaccount.DepartmentExternalIDEQ(departmentID)
}

func teamAccountIDIn(ids []int) predicate.TeamAccount {
	return entteamaccount.IDIn(ids...)
}

func teamAccountManagerUserID(userID int) predicate.TeamAccountManager {
	return entteamaccountmanager.UserIDEQ(userID)
}

func teamAccountManagerAccountID(accountID int) predicate.TeamAccountManager {
	return entteamaccountmanager.TeamAccountIDEQ(accountID)
}

func teamAccountKeyAccountID(accountID int) predicate.TeamAccountKey {
	return entteamaccountkey.TeamAccountIDEQ(accountID)
}

func teamAccountKeyIDEQ(keyID int) predicate.TeamAccountKey {
	return entteamaccountkey.IDEQ(keyID)
}

func teamAccountEventAccountID(accountID int) predicate.TeamAccountEvent {
	return entteamaccountevent.TeamAccountIDEQ(accountID)
}

// relayProviderEnabled scopes provider lookups to enabled sub2api entries.
// Usage-only entries (service_type=kiro2cc) are not relay providers and must
// never be picked by the primary-provider fallback.
func relayProviderEnabled() predicate.RelayProvider {
	return entrelayprovider.And(
		entrelayprovider.EnabledEQ(true),
		entrelayprovider.ServiceTypeEQ("sub2api"),
	)
}

func teamAccountKeyStatus(status string) predicate.TeamAccountKey {
	return entteamaccountkey.StatusEQ(entteamaccountkey.Status(status))
}
