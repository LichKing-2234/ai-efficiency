import type { LucideIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { cn } from '@/lib/utils'

export type SectionNavItem<T extends string> = {
  value: T
  label: React.ReactNode
  icon: LucideIcon
  trailing?: React.ReactNode
}

export function SectionNav<T extends string>({
  value,
  items,
  onChange,
  ariaLabel,
  className,
  scroll = 'none'
}: {
  value: T
  items: Array<SectionNavItem<T>>
  onChange: (value: T) => void
  ariaLabel: string
  className?: string
  scroll?: 'none' | 'workbench'
}) {
  return (
    <nav
      aria-label={ariaLabel}
      className={cn('flex flex-col gap-1', scroll === 'workbench' && 'max-h-[430px] overflow-y-auto', className)}
    >
      {items.map((item) => {
        const active = item.value === value
        const Icon = item.icon
        return (
          <Button
            aria-current={active ? 'page' : undefined}
            className={cn(
              'h-[38px] w-full justify-start gap-2.5 px-[11px] text-left font-medium text-[13px] shadow-none',
              active
                ? 'border-[var(--line)] bg-[var(--surface-inset)] text-foreground hover:bg-[var(--surface-inset)]'
                : 'border-transparent bg-transparent text-[var(--ink-2)] hover:bg-[var(--surface)] hover:text-foreground'
            )}
            data-active={active}
            key={item.value}
            onClick={() => onChange(item.value)}
            type='button'
            variant='ghost'
          >
            <Icon className={cn('size-4', active ? 'text-[var(--ai)]' : 'text-[var(--ink-3)]')} />
            <span className='min-w-0 truncate'>{item.label}</span>
            {item.trailing ? <span className='ml-auto shrink-0 text-[11px] text-[var(--ink-3)]'>{item.trailing}</span> : null}
          </Button>
        )
      })}
    </nav>
  )
}

export function SectionNavFrame({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <Card className={cn('border border-[var(--line)] bg-[var(--surface-2)] p-[8px] shadow-none', className)} data-slot='section-nav-frame'>
      {children}
    </Card>
  )
}
