import type * as React from 'react'

export function AuthSurfaceActions({ children }: { children: React.ReactNode }) {
  return (
    <div className='border-border border-t px-[18px] py-[12px]' data-slot='auth-surface-actions'>
      {children}
    </div>
  )
}
