import { getAdminTeamAccountGroupOptions, updateAdminTeamAccountGroups } from '@/api/teamAccounts'

/** Loads the Relay groups an administrator may keep or add for an account. */
export async function loadGroupOptions() {
  const response = await getAdminTeamAccountGroupOptions()
  return response.data.data?.groups ?? []
}

/** Reconciles an account with the selected groups; unchecked groups are revoked. */
export async function saveAccountGroups(accountID: number, groupIDs: number[]) {
  return updateAdminTeamAccountGroups(accountID, groupIDs)
}
