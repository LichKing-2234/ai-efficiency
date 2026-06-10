import type * as React from 'react'
import { SectionCardHeader } from '@/components/primitives/section-card-header'
import { cn } from '@/lib/utils'

export function WorkbenchRail({
  actions,
  children,
  className,
  contentClassName,
  title
}: {
  actions?: React.ReactNode
  children: React.ReactNode
  className?: string
  contentClassName?: string
  title: React.ReactNode
}) {
  return (
    <aside className={cn('border-border bg-[var(--surface-2)] p-3 lg:border-r', className)} data-slot='workbench-rail'>
      <div data-slot='workbench-rail-header'>
        <SectionCardHeader
          actions={actions}
          className='px-0 pt-0 pb-3'
          title={title}
        />
      </div>
      <div className={cn('min-w-0', contentClassName)} data-slot='workbench-rail-content'>
        {children}
      </div>
    </aside>
  )
}
