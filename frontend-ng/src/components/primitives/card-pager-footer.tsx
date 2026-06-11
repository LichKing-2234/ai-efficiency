import type * as React from 'react'
import { ActionGroup } from '@/components/primitives/action-group'
import { CardFooter } from '@/components/ui/card'
import { cn } from '@/lib/utils'

export function CardPagerFooter({
  meta,
  summary,
  previous,
  next,
  className
}: {
  meta?: React.ReactNode
  summary: React.ReactNode
  previous: React.ReactNode
  next: React.ReactNode
  className?: string
}) {
  return (
    <CardFooter className={className}>
      <ActionGroup align='responsive-end' className='w-full items-center text-[12px]' dataSlot='card-pager-footer' fit layout='split' wrap>
        <span className='text-[12px] text-[var(--ink-3)]'>{summary}</span>
        <ActionGroup dataSlot='card-pager-footer-actions'>
          {previous}
          {meta ? <span className='text-[11.5px] text-[var(--ink-3)]' data-slot='card-pager-footer-meta'>{meta}</span> : null}
          {next}
        </ActionGroup>
      </ActionGroup>
    </CardFooter>
  )
}
