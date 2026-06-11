import { SearchIcon, XIcon } from 'lucide-react'
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from '@/components/ui/input-group'
import { cn } from '@/lib/utils'

export function SearchField({
  value,
  onChange,
  onClear,
  placeholder,
  ariaLabel,
  clearLabel,
  className,
  width = 'auto'
}: {
  value: string
  onChange: (value: string) => void
  onClear: () => void
  placeholder: string
  ariaLabel: string
  clearLabel: string
  className?: string
  width?: 'auto' | 'toolbar'
}) {
  return (
    <InputGroup className={cn('h-[34px] min-w-0 rounded-[var(--r-md)] border-[var(--line)] bg-[var(--surface-inset)] px-[1px] shadow-none', width === 'toolbar' && 'max-w-[320px] flex-1', className)}>
      <InputGroupAddon>
        <SearchIcon />
      </InputGroupAddon>
      <InputGroupInput
        aria-label={ariaLabel}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        type='search'
        value={value}
      />
      {value ? (
        <InputGroupAddon align='inline-end'>
          <InputGroupButton aria-label={clearLabel} onClick={onClear} size='icon-xs'>
            <XIcon />
          </InputGroupButton>
        </InputGroupAddon>
      ) : null}
    </InputGroup>
  )
}
