import type * as React from 'react'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'

export function TextField({
  className,
  controlClassName,
  disabled,
  id,
  label,
  multiline = false,
  onChange,
  placeholder,
  readOnly,
  type,
  value
}: {
  className?: string
  controlClassName?: string
  disabled?: boolean
  id: string
  label: React.ReactNode
  multiline?: boolean
  onChange?: (value: string) => void
  placeholder?: string
  readOnly?: boolean
  type?: React.HTMLInputTypeAttribute
  value: string
}) {
  return (
    <Field className={className}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      {multiline ? (
        <Textarea
          id={id}
          className={controlClassName}
          disabled={disabled}
          placeholder={placeholder}
          readOnly={readOnly}
          value={value}
          onChange={(event) => onChange?.(event.target.value)}
        />
      ) : (
        <Input
          id={id}
          className={controlClassName}
          disabled={disabled}
          placeholder={placeholder}
          readOnly={readOnly}
          type={type}
          value={value}
          onChange={(event) => onChange?.(event.target.value)}
        />
      )}
    </Field>
  )
}
