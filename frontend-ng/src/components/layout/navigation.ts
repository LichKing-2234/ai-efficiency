import {
  ActivityIcon,
  FolderGit2Icon,
  HomeIcon,
  type LucideIcon,
  SettingsIcon,
  ShieldIcon,
  UserIcon
} from 'lucide-react'
import type { MessageKey } from '@/lib/i18n/messages'

type NavItem = {
  to: '/' | '/events' | '/repos' | '/user' | '/admin/users' | '/settings'
  labelKey: MessageKey
  titleKey: MessageKey
  sectionKey: MessageKey
  section: 'analyze' | 'code' | 'account' | 'admin' | 'auth'
  icon: LucideIcon
  admin?: boolean
}

export const navItems = [
  { to: '/', labelKey: 'nav.overview', titleKey: 'nav.overview', sectionKey: 'nav.analyzeSection', section: 'analyze', icon: HomeIcon },
  { to: '/events', labelKey: 'nav.usageRecords', titleKey: 'nav.usageRecords', sectionKey: 'nav.analyzeSection', section: 'analyze', icon: ActivityIcon },
  { to: '/repos', labelKey: 'nav.codeRepositories', titleKey: 'nav.codeRepositories', sectionKey: 'nav.codeSection', section: 'code', icon: FolderGit2Icon },
  { to: '/user', labelKey: 'nav.mySetup', titleKey: 'nav.mySetup', sectionKey: 'nav.accountSection', section: 'account', icon: UserIcon },
  { to: '/admin/users', labelKey: 'nav.userManagement', titleKey: 'nav.userManagement', sectionKey: 'nav.adminSection', section: 'admin', icon: ShieldIcon, admin: true },
  { to: '/settings', labelKey: 'nav.adminConsole', titleKey: 'nav.adminConsole', sectionKey: 'nav.adminSection', section: 'admin', icon: SettingsIcon, admin: true }
] satisfies NavItem[]

export function pageMeta(pathname: string): Pick<NavItem, 'titleKey' | 'sectionKey'> {
  if (pathname.startsWith('/repos/') && pathname !== '/repos') return { titleKey: 'nav.repositoryDetail', sectionKey: 'nav.codeSection' }
  if (pathname.startsWith('/oauth/')) return { titleKey: 'nav.authSection', sectionKey: 'nav.authSection' }
  const match = navItems.find((item) => item.to === pathname) ?? navItems[0]
  return { titleKey: match.titleKey, sectionKey: match.sectionKey }
}
