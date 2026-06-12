import type * as React from 'react'
import { FieldDescription } from '@/components/ui/field'
import { StartActions } from '@/components/primitives/start-actions'

export function StartActionsFeedback({
  actions,
  children,
  description,
  hint,
  status
}: {
  actions: React.ReactNode
  children?: React.ReactNode
  description?: React.ReactNode
  hint?: React.ReactNode
  status?: React.ReactNode
}) {
  return (
    <>
      {description ? <FieldDescription>{description}</FieldDescription> : null}
      {children}
      <StartActions>
        {actions}
        {hint ? <FieldDescription>{hint}</FieldDescription> : null}
        {status}
      </StartActions>
    </>
  )
}
