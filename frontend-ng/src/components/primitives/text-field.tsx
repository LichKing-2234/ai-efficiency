import type * as React from 'react'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

const textFieldWidthClass = {
  default: undefined,
  datetime: 'w-[220px]',
  toolbar: 'w-[220px]',
  wide: 'w-72'
} as const

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
  value,
  width = 'default'
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
  width?: keyof typeof textFieldWidthClass
}) {
  return (
    <Field className={cn(textFieldWidthClass[width], className)}>
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
