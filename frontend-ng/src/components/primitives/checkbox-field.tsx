import type * as React from 'react'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldLabel } from '@/components/ui/field'

export function CheckboxField({
  checked,
  className,
  disabled,
  id,
  label,
  onCheckedChange
}: {
  checked: boolean
  className?: string
  disabled?: boolean
  id: string
  label: React.ReactNode
  onCheckedChange: (checked: boolean) => void
}) {
  return (
    <Field orientation='horizontal' className={className}>
      <Checkbox
        id={id}
        checked={checked}
        disabled={disabled}
        onCheckedChange={(value) => onCheckedChange(value === true)}
      />
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
    </Field>
  )
}
