import type * as React from 'react'
import { cn } from '@/lib/utils'

export function CommandFooter({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <div data-slot='command-footer' className={cn('border-t border-border px-4 py-2.5 text-[11px] text-[var(--ink-4)]', className)}>
      {children}
    </div>
  )
}
