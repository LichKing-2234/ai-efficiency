import type * as React from 'react'
import { SectionCardHeader } from '@/components/primitives/section-card-header'
import { cn } from '@/lib/utils'

export function WorkbenchRail({
  actions,
  children,
  className,
  contentClassName,
  description,
  title
}: {
  actions?: React.ReactNode
  children: React.ReactNode
  className?: string
  contentClassName?: string
  description?: React.ReactNode
  title: React.ReactNode
}) {
  return (
    <aside className={cn('border-border bg-[var(--surface-2)] px-[12px] py-[14px] min-[920px]:border-r', className)} data-slot='workbench-rail'>
      <div data-slot='workbench-rail-header'>
        <SectionCardHeader
          actions={actions}
          className='gap-0 px-0 pt-0 pb-3'
          description={description}
          title={title}
        />
      </div>
      <div className={cn('min-w-0', contentClassName)} data-slot='workbench-rail-content'>
        {children}
      </div>
    </aside>
  )
}

export function WorkbenchContent({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <section className={cn('min-w-0', className)} data-slot='workbench-content'>
      {children}
    </section>
  )
}
