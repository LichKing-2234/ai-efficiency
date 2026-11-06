import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { login } from '@/api/auth'
import {
  revealAdminTeamAccountCredentials,
  revealTeamAccountCredentials,
} from '@/api/teamAccounts'
import { replaceBrowserSession } from '@/auth/browserSession'
import { stashImpersonationReturn } from '@/auth/impersonation'
import { useI18n } from '@/i18n'
import type { TeamAccountSummary } from '@/types'

type AccountMode = 'user' | 'admin'

/**
 * Signs the operator in as a team account.
 *
 * The account has no interactive login of its own, so the browser exchanges the
 * account's Relay credentials for a normal session, keeps the operator session
 * aside, and the banner restores it afterwards.
 */
export function useTeamAccountLogin() {
  const { t } = useI18n()
  const pendingAccountID = ref<number | null>(null)

  async function loginAs(account: TeamAccountSummary, mode: AccountMode = 'user') {
    if (pendingAccountID.value !== null) return
    pendingAccountID.value = account.id
    try {
      const credentialResponse = mode === 'admin'
        ? await revealAdminTeamAccountCredentials(account.id)
        : await revealTeamAccountCredentials(account.id)
      const credentials = credentialResponse.data.data
      if (!credentials) throw new Error('credentials unavailable')

      const loginResponse = await login({
        username: credentials.email || credentials.username,
        password: credentials.password,
        source: 'sso',
      })
      const payload = loginResponse.data.data
      const accessToken = payload?.tokens?.access_token || payload?.token
      if (!accessToken) throw new Error('login response missing token')

      if (!stashImpersonationReturn(account.name)) {
        throw new Error('current session cannot be restored')
      }
      replaceBrowserSession({
        accessToken,
        refreshToken: payload?.tokens?.refresh_token || payload?.refresh_token || null,
      })
      window.location.assign('/usage')
    } catch (error: unknown) {
      const message = error instanceof Error && error.message
        ? error.message
        : String(error)
      ElMessage.error(t('teamAccounts.loginAsFailed', { message }))
    } finally {
      pendingAccountID.value = null
    }
  }

  return { loginAs, pendingAccountID }
}
