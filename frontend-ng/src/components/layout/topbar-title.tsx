import type * as React from 'react'
import { cn } from '@/lib/utils'

export function TopbarTitle({
  className,
  section,
  title
}: {
  className?: string
  section: React.ReactNode
  title: React.ReactNode
}) {
  return (
    <div className={cn('min-w-0', className)} data-slot='topbar-title'>
      <div className='font-semibold text-[10.5px] text-[var(--ink-4)] uppercase tracking-[0.04em]' data-slot='topbar-title-section'>
        {section}
      </div>
      <div className='truncate font-semibold text-[15px] leading-tight' data-slot='topbar-title-text'>
        {title}
      </div>
    </div>
  )
}
