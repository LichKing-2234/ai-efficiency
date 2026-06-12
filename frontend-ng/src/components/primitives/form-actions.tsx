import type * as React from 'react'
import { ActionGroup } from '@/components/primitives/action-group'

export function FormActions({
  align = 'responsive-end',
  children,
  wrap = false
}: {
  align?: 'start' | 'responsive-end'
  children: React.ReactNode
  wrap?: boolean
}) {
  return <ActionGroup align={align} dataSlot='form-actions' wrap={wrap}>{children}</ActionGroup>
}
