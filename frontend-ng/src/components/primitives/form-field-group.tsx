import type * as React from 'react'
import { FieldGroup } from '@/components/ui/field'

export function FormFieldGroup({
  children,
  className,
  gap
}: React.ComponentProps<typeof FieldGroup>) {
  return (
    <FieldGroup className={className} gap={gap}>
      {children}
    </FieldGroup>
  )
}
