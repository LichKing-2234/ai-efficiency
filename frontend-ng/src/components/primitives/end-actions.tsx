import type * as React from 'react'
import { ActionGroup } from '@/components/primitives/action-group'

export function EndActions({ children }: { children: React.ReactNode }) {
  return <ActionGroup push wrap className='min-h-9' dataSlot='end-actions'>{children}</ActionGroup>
}
