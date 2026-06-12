import type * as React from 'react'
import { ActionGroup } from '@/components/primitives/action-group'
import { Stack } from '@/components/primitives/stack'
import { CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'

function EntityCardTitle({ children }: { children: React.ReactNode }) {
  return (
    <CardTitle className='text-[14px] font-[650] leading-none'>
      {children}
    </CardTitle>
  )
}

function EntityCardDescription({ children }: { children: React.ReactNode }) {
  return (
    <CardDescription className='mt-0.5 break-words text-[12px] text-[var(--ink-3)]' data-slot='entity-card-description'>
      {children}
    </CardDescription>
  )
}

export function EntityCardHeader({
  title,
  description,
  leading,
  actions,
  className,
  contentClassName
}: {
  title: React.ReactNode
  description?: React.ReactNode
  leading?: React.ReactNode
  actions?: React.ReactNode
  className?: string
  contentClassName?: string
}) {
  return (
    <CardHeader data-slot='entity-card-header' className={cn('gap-4', className)}>
      <ActionGroup align='responsive-end' className={cn('min-[920px]:items-start', contentClassName)} dataSlot='entity-card-header-content' fit layout='split'>
        <ActionGroup align='start' className='min-w-0 gap-4' dataSlot='entity-card-header-identity' fit>
          {leading ? <div className='shrink-0'>{leading}</div> : null}
          <Stack className='min-w-0' dataSlot='entity-card-header-copy' gap='none'>
            <EntityCardTitle>{title}</EntityCardTitle>
            {description ? <EntityCardDescription>{description}</EntityCardDescription> : null}
          </Stack>
        </ActionGroup>
        {actions ? <ActionGroup align='responsive-end' className='shrink-0' dataSlot='entity-card-header-actions' wrap>{actions}</ActionGroup> : null}
      </ActionGroup>
    </CardHeader>
  )
}
