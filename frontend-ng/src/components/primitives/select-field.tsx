import { Field, FieldLabel } from '@/components/ui/field'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'

export interface SelectFieldOption {
  disabled?: boolean
  label: React.ReactNode
  value: string
}

export function SelectField({
  className,
  disabled,
  id,
  label,
  onValueChange,
  options,
  placeholder,
  triggerClassName,
  value
}: {
  className?: string
  disabled?: boolean
  id: string
  label: React.ReactNode
  onValueChange: (value: string) => void
  options: SelectFieldOption[]
  placeholder?: string
  triggerClassName?: string
  value: string
}) {
  const selected = options.find((option) => option.value === value)

  return (
    <Field className={className}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Select value={value} disabled={disabled} onValueChange={onValueChange}>
        <SelectTrigger id={id} className={triggerClassName} aria-label={typeof selected?.label === 'string' ? selected.label : undefined}>
          <SelectValue placeholder={placeholder} />
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
    </Field>
  )
}
