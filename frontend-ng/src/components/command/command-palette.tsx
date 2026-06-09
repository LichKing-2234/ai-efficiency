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
import { useEffect, useMemo, useRef, useState } from 'react'
import type { MessageKey } from '@/lib/i18n/messages'
import { useI18n } from '@/lib/i18n/i18n'
import { cn } from '@/lib/utils'

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
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)

  const commands = useMemo(() => COMMANDS.filter((command) => !command.admin || isAdmin), [isAdmin])
  const filtered = useMemo(() => {
    const normalized = query.trim().toLowerCase()
    if (!normalized) return commands
    return commands.filter((command) => t(command.labelKey).toLowerCase().includes(normalized))
  }, [commands, query, t])

  useEffect(() => {
    if (!open) return
    setQuery('')
    setSelected(0)
    window.setTimeout(() => inputRef.current?.focus(), 30)
  }, [open])

  useEffect(() => {
    setSelected(0)
  }, [query])

  useEffect(() => {
    if (!open) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose, open])

  async function run(command: CommandItem) {
    onClose()
    await navigate({ to: command.to })
  }

  if (!open) return null

  let index = -1
  const grouped = filtered.reduce<Record<string, typeof filtered>>((acc, command) => {
    const group = t(command.groupKey)
    acc[group] = acc[group] ?? []
    acc[group].push(command)
    return acc
  }, {})

  return (
    <div className='fixed inset-0 z-50 flex justify-center bg-[color-mix(in_oklab,var(--bg-sunken)_55%,transparent)] px-4 pt-[13vh] backdrop-blur-[3px]' onClick={onClose}>
      <div
        className='flex max-h-[60vh] w-[min(580px,92vw)] flex-col overflow-hidden rounded-[var(--r-lg)] border border-[var(--line-strong)] bg-popover shadow-[var(--sh-xl)] motion-safe:animate-[scale-in_.2s_var(--ease-out)_both]'
        onClick={(event) => event.stopPropagation()}
      >
        <div className='flex items-center gap-3 border-b border-border px-4 py-3.5'>
          <SearchIcon className='size-4.5 text-[var(--ink-3)]' />
          <input
            className='min-w-0 flex-1 bg-transparent text-[15px] outline-none placeholder:text-[var(--ink-3)]'
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'ArrowDown') {
                event.preventDefault()
                setSelected((value) => Math.min(filtered.length - 1, value + 1))
              } else if (event.key === 'ArrowUp') {
                event.preventDefault()
                setSelected((value) => Math.max(0, value - 1))
              } else if (event.key === 'Enter') {
                event.preventDefault()
                const command = filtered[selected]
                if (command) void run(command)
              }
            }}
            placeholder={t('command.placeholder')}
            ref={inputRef}
            value={query}
          />
          <kbd className='rounded border border-border bg-[var(--surface)] px-1.5 py-0.5 font-mono font-semibold text-[10.5px] text-[var(--ink-3)]'>esc</kbd>
        </div>
        <div className='min-h-0 overflow-y-auto p-2'>
          {filtered.length === 0 ? <div className='px-4 py-8 text-center text-muted-foreground text-sm'>{t('command.noResults')}</div> : null}
          {Object.entries(grouped).map(([group, items]) => (
            <div className='mb-1' key={group}>
              <div className='px-2 py-1 font-bold text-[10px] text-[var(--ink-4)] uppercase tracking-[0.07em]'>{group}</div>
              {items.map((command) => {
                index += 1
                const active = index === selected
                const Icon = command.icon
                const ownIndex = index
                return (
                  <button
                    className={cn(
                      'flex h-9 w-full items-center gap-3 rounded-[var(--r-sm)] px-2.5 text-left transition-colors',
                      active ? 'bg-[var(--ai-soft)] text-foreground' : 'text-[var(--ink-2)] hover:bg-[var(--surface-inset)]'
                    )}
                    key={command.id}
                    onClick={() => void run(command)}
                    onMouseEnter={() => setSelected(ownIndex)}
                    type='button'
                  >
                    <Icon className={cn('size-4', active ? 'text-[var(--ai-deep)]' : 'text-[var(--ink-3)]')} />
                    <span className='min-w-0 flex-1 truncate font-medium text-[13.5px]'>{t(command.labelKey)}</span>
                    {active ? <ArrowRightIcon className='size-3.5 text-[var(--ai-deep)]' /> : null}
                  </button>
                )
              })}
            </div>
          ))}
        </div>
        <div className='border-t border-border px-4 py-2.5 text-[11px] text-[var(--ink-4)]'>{t('command.footer')}</div>
      </div>
    </div>
  )
}
