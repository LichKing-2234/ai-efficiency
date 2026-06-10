import { useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import {
  ActivityIcon,
  ArrowRightIcon,
  FolderGit2Icon,
  GaugeIcon,
  HomeIcon,
  MoonIcon,
  SearchIcon,
  SettingsIcon,
  ShieldIcon,
  UserIcon
} from 'lucide-react'
import { useMemo } from 'react'
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut
} from '@/components/ui/command'
import { CommandFooter } from '@/components/primitives/command-footer'
import { api } from '@/lib/api'
import type { RepoConfig } from '@/lib/api/types'
import type { MessageKey } from '@/lib/i18n/messages'
import { useI18n } from '@/lib/i18n/i18n'

type BaseCommandItem = {
  id: string
  kind: 'nav' | 'action' | 'repo'
  to?: '/' | '/usage' | '/events' | '/repos' | '/repos/$id' | '/user' | '/admin/users' | '/settings'
  labelKey?: MessageKey
  label?: string
  groupKey: MessageKey
  icon: typeof HomeIcon
  admin?: boolean
}

type NavCommandItem = BaseCommandItem & { kind: 'nav'; to: Exclude<BaseCommandItem['to'], '/repos/$id' | undefined>; labelKey: MessageKey }
type ActionCommandItem = BaseCommandItem & { kind: 'action'; labelKey: MessageKey; to?: undefined }
type RepoCommandItem = BaseCommandItem & { kind: 'repo'; to: '/repos/$id'; params: { id: string }; label: string }
type CommandItem = NavCommandItem | ActionCommandItem | RepoCommandItem

const COMMANDS: Array<NavCommandItem | ActionCommandItem> = [
  { id: 'overview', kind: 'nav', to: '/', labelKey: 'nav.overview', groupKey: 'command.navigate', icon: HomeIcon },
  { id: 'usage', kind: 'nav', to: '/usage', labelKey: 'nav.usageAnalytics', groupKey: 'command.navigate', icon: GaugeIcon },
  { id: 'events', kind: 'nav', to: '/events', labelKey: 'nav.usageRecords', groupKey: 'command.navigate', icon: ActivityIcon },
  { id: 'repos', kind: 'nav', to: '/repos', labelKey: 'nav.codeRepositories', groupKey: 'command.navigate', icon: FolderGit2Icon },
  { id: 'user', kind: 'nav', to: '/user', labelKey: 'nav.mySetup', groupKey: 'command.navigate', icon: UserIcon },
  { id: 'toggle-theme', kind: 'action', labelKey: 'nav.toggleTheme', groupKey: 'command.actions', icon: MoonIcon },
  { id: 'admin-users', kind: 'nav', to: '/admin/users', labelKey: 'nav.userManagement', groupKey: 'command.admin', icon: ShieldIcon, admin: true },
  { id: 'settings', kind: 'nav', to: '/settings', labelKey: 'nav.adminConsole', groupKey: 'command.admin', icon: SettingsIcon, admin: true }
]

function repoCommand(repo: RepoConfig): CommandItem {
  return {
    id: `repo-${repo.id}`,
    kind: 'repo',
    to: '/repos/$id',
    params: { id: String(repo.id) },
    label: repo.full_name || repo.name,
    groupKey: 'command.repositories',
    icon: FolderGit2Icon
  }
}

export function getCommandPaletteItems(isAdmin: boolean, repos: RepoConfig[] = []) {
  return [
    ...COMMANDS.filter((command) => !command.admin || isAdmin),
    ...repos.slice(0, 4).map(repoCommand)
  ]
}

export function CommandPalette({
  open,
  isAdmin,
  onClose,
  onToggleTheme
}: {
  open: boolean
  isAdmin: boolean
  onClose: () => void
  onToggleTheme: () => void
}) {
  const navigate = useNavigate()
  const { t } = useI18n()
  const repos = useQuery({
    queryKey: ['command-palette', 'repos'],
    queryFn: () => api.repos.list({ page: 1, pageSize: 4 }),
    enabled: open,
    staleTime: 60_000
  })
  const commands = useMemo(() => getCommandPaletteItems(isAdmin, repos.data?.items ?? []), [isAdmin, repos.data?.items])
  const grouped = useMemo(() => commands.reduce<Record<string, typeof commands>>((acc, command) => {
    const group = t(command.groupKey)
    acc[group] = acc[group] ?? []
    acc[group].push(command)
    return acc
  }, {}), [commands, t])

  async function run(command: CommandItem) {
    onClose()
    if (command.kind === 'action') {
      if (command.id === 'toggle-theme') onToggleTheme()
      return
    }
    if (command.to === '/repos/$id') {
      await navigate({ to: command.to, params: command.params })
      return
    }
    if (command.to) await navigate({ to: command.to })
  }

  function label(command: CommandItem) {
    return command.labelKey ? t(command.labelKey) : command.label ?? command.id
  }

  return (
    <CommandDialog
      description={t('command.placeholder')}
      onOpenChange={(value) => {
        if (!value) onClose()
      }}
      open={open}
      title={t('command.trigger')}
    >
      <Command>
        <CommandInput autoFocus placeholder={t('command.placeholder')} />
        <CommandList>
          <CommandEmpty>{t('command.noResults')}</CommandEmpty>
          {Object.entries(grouped).map(([group, items]) => (
            <CommandGroup heading={group} key={group}>
              {items.map((command) => {
                const Icon = command.icon
                return (
                  <CommandItem
                    key={command.id}
                    keywords={[t(command.groupKey), command.to ?? '', command.id]}
                    onSelect={() => void run(command)}
                    value={`${label(command)} ${command.id} ${command.to ?? ''}`}
                  >
                    <Icon />
                    <span className='min-w-0 flex-1 truncate font-medium text-[13.5px]'>{label(command)}</span>
                    <CommandShortcut>
                      <ArrowRightIcon />
                    </CommandShortcut>
                  </CommandItem>
                )
              })}
            </CommandGroup>
          ))}
        </CommandList>
        <CommandFooter>{t('command.footer')}</CommandFooter>
      </Command>
    </CommandDialog>
  )
}
