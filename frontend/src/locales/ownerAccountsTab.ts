/**
 * Tab-label slice of the owner account feature dictionary.
 *
 * The usage-center tabs render in the static `/usage` closure, so they must not
 * pull in the complete owner-account dictionary. The full dictionary lives in
 * `ownerAccounts.ts` and is loaded with the account console route.
 */
export const ownerAccountsTabMessages = {
  'en-US': {
    'ownerAccounts.tab': 'My Accounts',
  },
  'zh-CN': {
    'ownerAccounts.tab': '我的账号',
  },
} as const
