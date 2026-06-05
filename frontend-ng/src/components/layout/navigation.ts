import {
  ActivityIcon,
  FolderGit2Icon,
  HomeIcon,
  type LucideIcon,
  SettingsIcon,
  ShieldIcon,
  UserIcon
} from 'lucide-react'

type NavItem = {
  to: '/' | '/events' | '/repos' | '/user' | '/admin/users' | '/settings'
  label: string
  title: string
  section: 'Analyze' | 'Code & PR' | 'Account' | 'Administration' | 'Auth'
  icon: LucideIcon
  admin?: boolean
}

export const navItems = [
  { to: '/', label: 'Overview', section: 'Analyze', icon: HomeIcon },
  { to: '/events', label: 'Usage Records', section: 'Analyze', icon: ActivityIcon },
  { to: '/repos', label: 'Repositories', section: 'Code & PR', icon: FolderGit2Icon },
  { to: '/user', label: 'My Setup', section: 'Account', icon: UserIcon },
  { to: '/admin/users', label: 'User Management', section: 'Administration', icon: ShieldIcon, admin: true },
  { to: '/settings', label: 'Admin Console', section: 'Administration', icon: SettingsIcon, admin: true }
] satisfies Array<Omit<NavItem, 'title'>>

export function pageMeta(pathname: string): Pick<NavItem, 'title' | 'section'> {
  if (pathname.startsWith('/repos/') && pathname !== '/repos') return { title: 'Repository Detail', section: 'Code & PR' }
  if (pathname.startsWith('/oauth/')) return { title: 'OAuth', section: 'Auth' }
  const match = navItems.find((item) => item.to === pathname) ?? navItems[0]
  return { title: match.label, section: match.section }
}
