import type * as React from 'react'
import { ActionGroup } from '@/components/primitives/action-group'

export function StartActions({ children }: { children: React.ReactNode }) {
  return <ActionGroup align='start' dataSlot='start-actions' wrap>{children}</ActionGroup>
}
