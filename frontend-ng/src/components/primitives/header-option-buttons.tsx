import { ActionGroup } from '@/components/primitives/action-group'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

export type HeaderOptionButton<T extends string> = {
  label: React.ReactNode
  value: T
}

export function HeaderOptionButtons<T extends string>({
  ariaLabel,
  onChange,
  options,
  value
}: {
  ariaLabel?: string
  onChange: (value: T) => void
  options: Array<HeaderOptionButton<T>>
  value: T | null
}) {
  return (
    <ActionGroup
      align='responsive-end'
      aria-label={ariaLabel}
      className='rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] p-[3px]'
      dataSlot='header-option-buttons'
      wrap
    >
      {options.map((option) => (
        <Button
          key={option.value}
          className={cn(
            'h-6.5 px-3 text-[12.5px]',
            option.value === value
              ? 'bg-[var(--ink)] text-[var(--ink-on-accent)] hover:bg-[var(--ink)] hover:text-[var(--ink-on-accent)]'
              : 'border-transparent bg-transparent text-[var(--ink-2)] hover:border-transparent hover:bg-[var(--surface)] hover:text-[var(--ink)]'
          )}
          onClick={() => onChange(option.value)}
          size='sm'
          type='button'
          variant='ghost'
        >
          {option.label}
        </Button>
      ))}
    </ActionGroup>
  )
}
