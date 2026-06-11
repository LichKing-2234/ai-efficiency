import { Stack } from '@/components/primitives/stack'
import { cn } from '@/lib/utils'

export type OptionListItem = {
  id: string | number
  label: React.ReactNode
  description?: React.ReactNode
}

function OptionDescription({ children }: { children: React.ReactNode }) {
  return (
    <span className='block truncate text-[11px] text-[var(--ink-4)]' data-slot='option-description'>
      {children}
    </span>
  )
}

export function OptionList({
  ariaLabel,
  className,
  items,
  onSelect
}: {
  ariaLabel: string
  className?: string
  items: Array<OptionListItem>
  onSelect: (item: OptionListItem) => void
}) {
  return (
    <Stack
      aria-label={ariaLabel}
      className={cn('rounded-[var(--r-md)] border border-border bg-[var(--surface)] p-[10px]', className)}
      dataSlot='option-list'
      gap='none'
      role='listbox'
    >
      {items.map((item) => (
        <button
          className='min-w-0 rounded-[var(--r-sm)] px-[10px] py-[8px] text-left text-[12.5px] transition hover:bg-[var(--surface-inset)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'
          key={item.id}
          onClick={() => onSelect(item)}
          role='option'
          type='button'
        >
          <Stack dataSlot='option-copy' gap='none'>
            <span className='block truncate font-medium text-[12.5px]'>{item.label}</span>
            {item.description ? <OptionDescription>{item.description}</OptionDescription> : null}
          </Stack>
        </button>
      ))}
    </Stack>
  )
}
