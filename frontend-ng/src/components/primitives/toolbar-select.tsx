import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { cn } from '@/lib/utils'
import type { SelectFieldOption } from './select-field'

export function ToolbarSelect({
  ariaLabel,
  className,
  onValueChange,
  options,
  size,
  value
}: {
  ariaLabel: string
  className?: string
  onValueChange: (value: string) => void
  options: SelectFieldOption[]
  size?: 'default' | 'sm'
  value: string
}) {
  const selected = options.find((option) => option.value === value)

  return (
    <Select value={value} onValueChange={onValueChange}>
      <SelectTrigger
        aria-label={typeof selected?.label === 'string' ? `${ariaLabel}: ${selected.label}` : ariaLabel}
        className={cn('min-w-24', className)}
        size={size}
      >
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          {options.map((option) => (
            <SelectItem disabled={option.disabled} key={option.value} value={option.value}>
              {option.label}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}
