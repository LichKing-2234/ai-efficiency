import type * as React from 'react'
import { ActionGroup } from '@/components/primitives/action-group'

export function AuthSurfaceActions({ children }: { children: React.ReactNode }) {
  return (
    <ActionGroup className='border-border border-t px-[18px] py-[12px]' dataSlot='auth-surface-actions' layout='split'>
      {children}
    </ActionGroup>
  )
}
