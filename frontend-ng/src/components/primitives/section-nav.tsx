import type { LucideIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

export type SectionNavItem<T extends string> = {
  value: T
  label: React.ReactNode
  icon: LucideIcon
}

export function SectionNav<T extends string>({
  value,
  items,
  onChange,
  ariaLabel,
  className
}: {
  value: T
  items: Array<SectionNavItem<T>>
  onChange: (value: T) => void
  ariaLabel: string
  className?: string
}) {
  return (
    <nav aria-label={ariaLabel} className={cn('flex flex-col gap-1', className)}>
      {items.map((item) => {
        const active = item.value === value
        const Icon = item.icon
        return (
          <Button
            aria-current={active ? 'page' : undefined}
            className={cn(
              'h-10 w-full justify-start gap-3 px-3 text-left font-medium text-sm shadow-none',
              active
                ? 'border-transparent bg-[var(--surface-inset)] text-foreground hover:bg-[var(--surface-inset)]'
                : 'border-transparent bg-transparent text-[var(--ink-2)] hover:bg-[var(--surface-inset)] hover:text-foreground'
            )}
            data-active={active}
            key={item.value}
            onClick={() => onChange(item.value)}
            type='button'
            variant='ghost'
          >
            <Icon className={active ? 'text-[var(--ai)]' : 'text-[var(--ink-3)]'} />
            <span className='min-w-0 truncate'>{item.label}</span>
          </Button>
        )
      })}
    </nav>
  )
}
