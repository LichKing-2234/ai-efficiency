import { ChevronDownIcon, GlobeIcon, MoonIcon, SunIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import type { Locale } from '@/lib/i18n/messages'
import { TopbarCommandTrigger } from './topbar-command-trigger'
import { TopbarLiveStatus } from './topbar-live-status'

export type TopbarLocaleOption = {
  value: Locale
  label: string
}

export function TopbarActions({
  commandLabel,
  dark,
  ingestingLabel,
  locale,
  locales,
  onLocaleChange,
  onOpenCommand,
  onToggleTheme,
  themeLabel
}: {
  commandLabel: string
  dark: boolean
  ingestingLabel: string
  locale: Locale
  locales: TopbarLocaleOption[]
  onLocaleChange: (locale: Locale) => void
  onOpenCommand: () => void
  onToggleTheme: () => void
  themeLabel: string
}) {
  const currentLocale = locales.find((item) => item.value === locale) ?? locales[0]

  return (
    <div className='ml-auto flex items-center gap-[8px]' data-slot='topbar-actions'>
      <TopbarCommandTrigger label={commandLabel} onOpen={onOpenCommand} />
      <TopbarLiveStatus label={ingestingLabel} />
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            className='gap-[6px] px-[9px] text-[12px] font-semibold'
            data-slot='topbar-actions-locale-trigger'
            size='sm'
            type='button'
            variant='ghost'
          >
            <GlobeIcon />
            <span className='hidden min-[920px]:inline'>{currentLocale?.label}</span>
            <ChevronDownIcon className='text-[var(--ink-4)]' />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align='end' className='min-w-40 border-[var(--line-strong)]'>
          <DropdownMenuRadioGroup value={locale} onValueChange={(value) => onLocaleChange(value as Locale)}>
            {locales.map((item) => (
              <DropdownMenuRadioItem className='h-8 text-[13px]' key={item.value} value={item.value}>
                {item.label}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
      <Button aria-label={themeLabel} onClick={onToggleTheme} size='icon-sm' title={themeLabel} type='button' variant='ghost'>
        {dark ? <SunIcon /> : <MoonIcon />}
      </Button>
    </div>
  )
}
