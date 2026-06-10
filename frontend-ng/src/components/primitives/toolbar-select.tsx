import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { cn } from '@/lib/utils'
import type { SelectFieldOption } from './select-field'

const toolbarSelectWidthClass = {
  auto: 'min-w-24',
  compact: 'w-36',
  full: 'w-full',
  toolbar: 'w-[220px]'
} as const

export function ToolbarSelect({
  ariaLabel,
  className,
  disabled,
  onValueChange,
  options,
  size,
  value,
  width = 'auto'
}: {
  ariaLabel: string
  className?: string
  disabled?: boolean
  onValueChange: (value: string) => void
  options: SelectFieldOption[]
  size?: 'default' | 'sm'
  value: string
  width?: keyof typeof toolbarSelectWidthClass
}) {
  const selected = options.find((option) => option.value === value)

  return (
    <Select value={value} disabled={disabled} onValueChange={onValueChange}>
      <SelectTrigger
        aria-label={typeof selected?.label === 'string' ? `${ariaLabel}: ${selected.label}` : ariaLabel}
        className={cn(toolbarSelectWidthClass[width], className)}
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
