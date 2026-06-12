import type * as React from 'react'
import { ActionGroup } from '@/components/primitives/action-group'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { Stack } from '@/components/primitives/stack'
import { cn } from '@/lib/utils'

function HeroTitle({ children }: { children: React.ReactNode }) {
  return (
    <h1 className='text-[25px] font-[680] leading-[1.18] tracking-[-0.025em]' data-slot='hero-title'>
      {children}
    </h1>
  )
}

function HeroDescription({ children }: { children: React.ReactNode }) {
  return (
    <p className='text-[13.5px] leading-[1.5] text-[var(--ink-2)]' data-slot='hero-description'>
      {children}
    </p>
  )
}

export function HeroContent({
  action,
  badge,
  className,
  description,
  title
}: {
  action?: React.ReactNode
  badge?: React.ReactNode
  className?: string
  description?: React.ReactNode
  title: React.ReactNode
}) {
  return (
    <CardContentStack className={cn('p-[22px]', className)} dataSlot='hero-content'>
      <ActionGroup align='responsive-end' className='items-center gap-6' dataSlot='hero-shell' fit layout='split' wrap>
        <Stack className='max-w-[540px]' dataSlot='hero-copy' gap='compact'>
          {badge}
          <HeroTitle>{title}</HeroTitle>
          {description ? <HeroDescription>{description}</HeroDescription> : null}
        </Stack>
        {action ? <ActionGroup dataSlot='hero-action'>{action}</ActionGroup> : null}
      </ActionGroup>
    </CardContentStack>
  )
}
