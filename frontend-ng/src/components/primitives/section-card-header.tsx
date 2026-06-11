import type * as React from 'react'
import { ActionGroup } from '@/components/primitives/action-group'
import { Stack } from '@/components/primitives/stack'
import { CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

function SectionCardTitle({ children }: { children: React.ReactNode }) {
  return (
    <CardTitle className='text-[14px] font-[650] leading-none'>
      {children}
    </CardTitle>
  )
}

function SectionCardDescription({ children }: { children: React.ReactNode }) {
  return (
    <CardDescription className='mt-0.5 text-[12px] text-[var(--ink-3)]'>
      {children}
    </CardDescription>
  )
}

export function SectionCardHeader({
  title,
  description,
  actions,
  leading: Leading,
  live,
  meta,
  className
}: {
  title: React.ReactNode
  description?: React.ReactNode
  actions?: React.ReactNode
  leading?: React.ComponentType<{ className?: string }>
  live?: boolean
  meta?: React.ReactNode
  className?: string
}) {
  const titleNode = Leading || live ? (
    <ActionGroup align='start' className='min-w-0 gap-[9px]' dataSlot='section-card-title-row' fit>
      {live ? <span data-slot='section-card-live-indicator' className='live-dot' /> : null}
      {Leading ? <Leading data-slot='section-card-leading-icon' className='shrink-0 text-[var(--ai)]' /> : null}
      <span className='min-w-0 truncate'>{title}</span>
    </ActionGroup>
  ) : title

  return (
    <CardHeader className={className}>
      <ActionGroup
        align='responsive-end'
        className='w-full gap-3'
        dataSlot='section-card-header-content'
        fit
        layout='split'
        wrap={Boolean(actions)}
      >
        <SectionCardTitle>{titleNode}</SectionCardTitle>
        {meta || actions ? (
          <ActionGroup align='responsive-end' className='shrink-0 gap-2.5' dataSlot='section-card-header-actions'>
            {meta ? <span className='text-[12px] text-[var(--ink-3)]' data-slot='section-card-meta'>{meta}</span> : null}
            {actions}
          </ActionGroup>
        ) : null}
      </ActionGroup>
      {description ? <SectionCardDescription>{description}</SectionCardDescription> : null}
    </CardHeader>
  )
}
