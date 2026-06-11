import type * as React from 'react'
import { ActionGroup } from '@/components/primitives/action-group'
import { Card } from '@/components/ui/card'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { SectionCardHeader } from '@/components/primitives/section-card-header'
import { cn } from '@/lib/utils'

export function AuthSurface({
  actions,
  aside,
  children,
  className,
  description,
  title
}: {
  actions?: React.ReactNode
  aside?: React.ReactNode
  children: React.ReactNode
  className?: string
  description: string
  title: string
}) {
  return (
    <main
      data-slot='auth-surface'
      className='grid min-h-screen place-items-center bg-[radial-gradient(120%_140%_at_88%_-10%,var(--ai-softer),transparent_55%),var(--bg)] p-[18px]'
    >
      <Card className={cn('grid-paper w-full max-w-[448px] overflow-hidden border-[var(--ai-line)]', className)} variant='accent'>
        <SectionCardHeader className='px-[18px] pt-[18px]' title={title} description={description} />
        <CardContentStack className='border-border border-t px-[18px] py-[18px]'>
          {aside}
          {children}
        </CardContentStack>
        {actions ? (
          <ActionGroup className='px-[18px] pb-[18px]' dataSlot='auth-surface-actions' layout='split'>
            {actions}
          </ActionGroup>
        ) : null}
      </Card>
    </main>
  )
}
