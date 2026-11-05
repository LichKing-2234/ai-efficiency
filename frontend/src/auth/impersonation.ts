import { readBrowserSession, replaceBrowserSession } from './browserSession'

// Impersonation keeps the operator's session in a side slot so a team account
// session can be dropped in for a few minutes and reverted without a password.
const RETURN_KEY = 'ae.impersonation.return'

export interface ImpersonationReturn {
  accountName: string
  accessToken: string
  refreshToken: string | null
}

export function readImpersonationReturn(): ImpersonationReturn | null {
  try {
    const raw = localStorage.getItem(RETURN_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<ImpersonationReturn>
    if (!parsed || typeof parsed.accessToken !== 'string' || !parsed.accessToken) return null
    return {
      accountName: typeof parsed.accountName === 'string' ? parsed.accountName : '',
      accessToken: parsed.accessToken,
      refreshToken: typeof parsed.refreshToken === 'string' ? parsed.refreshToken : null,
    }
  } catch {
    return null
  }
}

/** Stores the current session so it can be restored after impersonation. */
export function stashImpersonationReturn(accountName: string): boolean {
  const session = readBrowserSession()
  if (!session.accessToken) return false
  const payload: ImpersonationReturn = {
    accountName,
    accessToken: session.accessToken,
    refreshToken: session.refreshToken,
  }
  localStorage.setItem(RETURN_KEY, JSON.stringify(payload))
  return true
}

export function clearImpersonationReturn() {
  localStorage.removeItem(RETURN_KEY)
}

/** Restores the stashed operator session; returns false when none is stored. */
export function switchBackFromImpersonation(): boolean {
  const saved = readImpersonationReturn()
  if (!saved) return false
  replaceBrowserSession({ accessToken: saved.accessToken, refreshToken: saved.refreshToken })
  clearImpersonationReturn()
  return true
}
