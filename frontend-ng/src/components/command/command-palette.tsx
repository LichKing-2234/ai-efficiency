import { useNavigate } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ActivityIcon,
  ArrowRightIcon,
  DownloadIcon,
  FolderGit2Icon,
  GaugeIcon,
  KeyIcon,
  HomeIcon,
  MoonIcon,
  PlusIcon,
  SearchIcon,
  SettingsIcon,
  ShieldIcon,
  WandSparklesIcon,
  UserIcon
} from 'lucide-react'
import { useMemo } from 'react'
import { toast } from 'sonner'
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
import { cn } from '@/lib/utils'

type BaseCommandItem = {
  id: string
  kind: 'nav' | 'action' | 'repo'
  to?: '/' | '/usage' | '/events' | '/repos' | '/repos/$id' | '/user' | '/admin/users' | '/settings'
  labelKey?: MessageKey
  label?: string
  meta?: string
  groupKey: MessageKey
  icon: typeof HomeIcon
  admin?: boolean
}

type NavCommandItem = BaseCommandItem & { kind: 'nav'; to: Exclude<BaseCommandItem['to'], '/repos/$id' | undefined>; labelKey: MessageKey }
type ActionCommandItem = BaseCommandItem & { kind: 'action'; labelKey: MessageKey; to?: Exclude<BaseCommandItem['to'], '/repos/$id' | undefined> }
type RepoCommandItem = BaseCommandItem & { kind: 'repo'; to: '/repos/$id'; params: { id: string }; label: string }
type CommandItem = NavCommandItem | ActionCommandItem | RepoCommandItem

const COMMANDS: Array<NavCommandItem | ActionCommandItem> = [
  { id: 'overview', kind: 'nav', to: '/', labelKey: 'nav.overview', meta: '/', groupKey: 'command.navigate', icon: HomeIcon },
  { id: 'usage', kind: 'nav', to: '/usage', labelKey: 'nav.usageAnalytics', meta: '/usage', groupKey: 'command.navigate', icon: GaugeIcon },
  { id: 'events', kind: 'nav', to: '/events', labelKey: 'nav.usageRecords', meta: '/events', groupKey: 'command.navigate', icon: ActivityIcon },
  { id: 'repos', kind: 'nav', to: '/repos', labelKey: 'nav.codeRepositories', meta: '/repos', groupKey: 'command.navigate', icon: FolderGit2Icon },
  { id: 'user', kind: 'nav', to: '/user', labelKey: 'nav.mySetup', meta: '/user', groupKey: 'command.navigate', icon: UserIcon },
  { id: 'add-repository', kind: 'action', to: '/repos', labelKey: 'command.addRepository', meta: 'command.meta.opensRepositories', groupKey: 'command.actions', icon: PlusIcon },
  { id: 'create-api-key', kind: 'action', to: '/user', labelKey: 'command.createApiKey', meta: 'command.meta.opensMySetup', groupKey: 'command.actions', icon: KeyIcon },
  { id: 'export-usage-report', kind: 'action', to: '/usage', labelKey: 'command.exportUsageReport', meta: 'command.meta.opensUsageAnalytics', groupKey: 'command.actions', icon: DownloadIcon },
  { id: 'auto-bind-unbound', kind: 'action', labelKey: 'repos.autoBind', meta: 'command.meta.mutatesRepositories', groupKey: 'command.actions', icon: WandSparklesIcon, admin: true },
  { id: 'toggle-theme', kind: 'action', labelKey: 'nav.toggleTheme', meta: 'command.meta.updatesAppearance', groupKey: 'command.actions', icon: MoonIcon },
  { id: 'admin-users', kind: 'nav', to: '/admin/users', labelKey: 'nav.userManagement', meta: '/admin/users', groupKey: 'command.admin', icon: ShieldIcon, admin: true },
  { id: 'settings', kind: 'nav', to: '/settings', labelKey: 'nav.adminConsole', meta: '/settings', groupKey: 'command.admin', icon: SettingsIcon, admin: true }
]

function repoCommand(repo: RepoConfig): CommandItem {
  return {
    id: `repo-${repo.id}`,
    kind: 'repo',
    to: '/repos/$id',
    params: { id: String(repo.id) },
    label: repo.full_name || repo.name,
    meta: repo.default_branch ? `${repo.default_branch} branch` : repo.clone_url,
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
  const qc = useQueryClient()
  const { t } = useI18n()
  const repos = useQuery({
    queryKey: ['command-palette', 'repos'],
    queryFn: () => api.repos.list({ page: 1, pageSize: 4 }),
    enabled: open,
    staleTime: 60_000
  })
  const autoBind = useMutation({
    mutationFn: api.repos.autoBindUnbound,
    onSuccess: (result) => {
      toast.success(t('repos.autoBindSummary', {
        bound: result.summary.bound,
        noMatch: result.summary.skipped_no_match,
        ambiguous: result.summary.skipped_ambiguous,
        webhookFailed: result.summary.webhook_failed,
        errors: result.summary.errors
      }))
      void qc.invalidateQueries({ queryKey: ['repos'] })
      void qc.invalidateQueries({ queryKey: ['command-palette', 'repos'] })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t('repos.autoBindFailed'))
    }
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
      if (command.id === 'auto-bind-unbound') autoBind.mutate()
      if (command.to) await navigate({ to: command.to })
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

  function meta(command: CommandItem) {
    if (!command.meta) return null
    if (command.meta.startsWith('command.')) return t(command.meta as MessageKey)
    return command.meta
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
                    className={cn('h-auto min-h-10 py-2', command.kind === 'repo' && 'items-start')}
                    keywords={[t(command.groupKey), command.to ?? '', command.id, meta(command) ?? '']}
                    onSelect={() => void run(command)}
                    value={`${label(command)} ${command.id} ${command.to ?? ''}`}
                  >
                    <Icon />
                    <span className='min-w-0 flex-1'>
                      <span className='block truncate font-medium text-[13.5px]'>{label(command)}</span>
                      {meta(command) ? (
                        <span className='block truncate pt-0.5 text-[11.5px] text-[var(--ink-4)]'>
                          {meta(command)}
                        </span>
                      ) : null}
                    </span>
                    <CommandShortcut>
                      <ArrowRightIcon />
                    </CommandShortcut>
                  </CommandItem>
                )
              })}
            </CommandGroup>
          ))}
        </CommandList>
        <CommandFooter>
          <CommandFooter.Hint keys={<CommandFooter.Key>↑↓</CommandFooter.Key>} label={t('command.navigateHint')} />
          <CommandFooter.Hint keys={<CommandFooter.Key>↵</CommandFooter.Key>} label={t('command.selectHint')} />
          <span className='ml-auto flex items-center gap-1.5' data-slot='command-footer-brand'>
            <WandSparklesIcon className='size-3 text-[var(--ai)]' />
            <span>{t('app.title')}</span>
          </span>
        </CommandFooter>
      </Command>
    </CommandDialog>
  )
}
