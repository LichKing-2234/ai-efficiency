import { cn } from '@/lib/utils'

export type SegmentedOption<T extends string> = {
  value: T
  label: React.ReactNode
}

export function SegmentedControl<T extends string>({
  value,
  options,
  onChange,
  size = 'default',
  className,
  ariaLabel
}: {
  value: T
  options: Array<SegmentedOption<T>>
  onChange: (value: T) => void
  size?: 'default' | 'sm'
  className?: string
  ariaLabel?: string
}) {
  return (
    <div
      aria-label={ariaLabel}
      className={cn(
        'inline-flex items-center gap-0.5 rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] p-0.5',
        className
      )}
      role='radiogroup'
    >
      {options.map((option) => {
        const active = option.value === value
        return (
          <button
            aria-checked={active}
            className={cn(
              'rounded-[calc(var(--r-md)-3px)] border px-3 font-semibold text-[12.5px] transition-all duration-150 ease-[var(--ease-out)]',
              size === 'sm' ? 'h-7' : 'h-8',
              active
                ? 'border-border bg-[var(--surface)] text-foreground'
                : 'border-transparent text-[var(--ink-3)] hover:text-foreground'
            )}
            key={option.value}
            onClick={() => onChange(option.value)}
            role='radio'
            type='button'
          >
            {option.label}
          </button>
        )
      })}
    </div>
  )
}
