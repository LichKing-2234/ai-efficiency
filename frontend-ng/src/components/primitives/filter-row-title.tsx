import type * as React from 'react'
import { Stack } from '@/components/primitives/stack'
import { cn } from '@/lib/utils'

function FilterRowTitleDescription({ children }: { children: React.ReactNode }) {
  return (
    <div className='text-[12px] text-[var(--ink-3)]' data-slot='filter-row-title-description'>
      {children}
    </div>
  )
}

export function FilterRowTitle({
  className,
  description,
  title,
  variant = 'title'
}: {
  className?: string
  description?: React.ReactNode
  title: React.ReactNode
  variant?: 'label' | 'title'
}) {
  if (variant === 'label') {
    return (
      <Stack className={cn('min-w-0', className)} dataSlot='filter-row-title' gap='none'>
        <span className='text-[12px] text-[var(--ink-3)]' data-slot='filter-row-title-label'>{title}</span>
      </Stack>
    )
  }

  return (
    <Stack className={cn('min-w-0', className)} dataSlot='filter-row-title' gap='none'>
      <div className='font-semibold text-[14px]' data-slot='filter-row-title-text'>{title}</div>
      {description ? <FilterRowTitleDescription>{description}</FilterRowTitleDescription> : null}
    </Stack>
  )
}
