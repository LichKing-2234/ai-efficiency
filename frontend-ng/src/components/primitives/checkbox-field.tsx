import type * as React from 'react'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldLabel } from '@/components/ui/field'
import { cn } from '@/lib/utils'

type CheckboxFieldAlign = 'center' | 'block-end'

export function CheckboxField({
  align = 'center',
  checked,
  className,
  disabled,
  id,
  label,
  onCheckedChange
}: {
  align?: CheckboxFieldAlign
  checked: boolean
  className?: string
  disabled?: boolean
  id: string
  label: React.ReactNode
  onCheckedChange: (checked: boolean) => void
}) {
  return (
    <Field
      orientation='horizontal'
      data-align={align}
      className={cn(align === 'block-end' && 'min-h-14 items-end pb-1', className)}
    >
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
