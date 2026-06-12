import type * as React from 'react'
import { ActionGroup } from '@/components/primitives/action-group'

export function BlockEndActions({ children }: { children: React.ReactNode }) {
  return <ActionGroup align='block-end' dataSlot='block-end-actions'>{children}</ActionGroup>
}
