import type * as React from 'react'
import { HelperText } from '@/components/primitives/helper-text'
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
      {description ? <HelperText>{description}</HelperText> : null}
      {children}
      <StartActions>
        {actions}
        {hint ? <HelperText>{hint}</HelperText> : null}
        {status}
      </StartActions>
    </>
  )
}
