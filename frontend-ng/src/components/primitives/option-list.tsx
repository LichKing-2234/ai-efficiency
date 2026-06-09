import { cn } from '@/lib/utils'

export type OptionListItem = {
  id: string | number
  label: React.ReactNode
  description?: React.ReactNode
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
    <div
      aria-label={ariaLabel}
      className={cn('flex flex-col gap-1 rounded-[var(--r-md)] border border-border bg-card p-2 shadow-[var(--sh-sm)]', className)}
      data-slot='option-list'
      role='listbox'
    >
      {items.map((item) => (
        <button
          className='min-w-0 rounded-[var(--r-sm)] px-2 py-1.5 text-left text-sm transition hover:bg-[var(--surface-inset)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'
          key={item.id}
          onClick={() => onSelect(item)}
          role='option'
          type='button'
        >
          <span className='block truncate font-medium'>{item.label}</span>
          {item.description ? <span className='mt-0.5 block truncate text-muted-foreground text-xs'>{item.description}</span> : null}
        </button>
      ))}
    </div>
  )
}
