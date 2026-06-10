import { useNavigate } from '@tanstack/react-router'
import {
  ActivityIcon,
  ArrowRightIcon,
  FolderGit2Icon,
  GaugeIcon,
  HomeIcon,
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
import type { MessageKey } from '@/lib/i18n/messages'
import { useI18n } from '@/lib/i18n/i18n'

type CommandItem = {
  id: string
  to: '/' | '/usage' | '/events' | '/repos' | '/user' | '/admin/users' | '/settings'
  labelKey: MessageKey
  groupKey: MessageKey
  icon: typeof HomeIcon
  admin?: boolean
}

const COMMANDS: CommandItem[] = [
  { id: 'overview', to: '/', labelKey: 'nav.overview', groupKey: 'command.navigate', icon: HomeIcon },
  { id: 'usage', to: '/usage', labelKey: 'nav.usageAnalytics', groupKey: 'command.navigate', icon: GaugeIcon },
  { id: 'events', to: '/events', labelKey: 'nav.usageRecords', groupKey: 'command.navigate', icon: ActivityIcon },
  { id: 'repos', to: '/repos', labelKey: 'nav.codeRepositories', groupKey: 'command.navigate', icon: FolderGit2Icon },
  { id: 'user', to: '/user', labelKey: 'nav.mySetup', groupKey: 'command.navigate', icon: UserIcon },
  { id: 'admin-users', to: '/admin/users', labelKey: 'nav.userManagement', groupKey: 'command.admin', icon: ShieldIcon, admin: true },
  { id: 'settings', to: '/settings', labelKey: 'nav.adminConsole', groupKey: 'command.admin', icon: SettingsIcon, admin: true }
]

export function getCommandPaletteItems(isAdmin: boolean) {
  return COMMANDS.filter((command) => !command.admin || isAdmin)
}

export function CommandPalette({
  open,
  isAdmin,
  onClose
}: {
  open: boolean
  isAdmin: boolean
  onClose: () => void
}) {
  const navigate = useNavigate()
  const { t } = useI18n()
  const commands = useMemo(() => getCommandPaletteItems(isAdmin), [isAdmin])
  const grouped = useMemo(() => commands.reduce<Record<string, typeof commands>>((acc, command) => {
    const group = t(command.groupKey)
    acc[group] = acc[group] ?? []
    acc[group].push(command)
    return acc
  }, {}), [commands, t])

  async function run(command: CommandItem) {
    onClose()
    await navigate({ to: command.to })
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
                    keywords={[t(command.groupKey), command.to, command.id]}
                    onSelect={() => void run(command)}
                    value={`${t(command.labelKey)} ${command.id} ${command.to}`}
                  >
                    <Icon />
                    <span className='min-w-0 flex-1 truncate font-medium text-[13.5px]'>{t(command.labelKey)}</span>
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
