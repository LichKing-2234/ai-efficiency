import type * as React from 'react'
import { ActionGroup } from '@/components/primitives/action-group'
import { AppBrand } from '@/components/primitives/app-brand'
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
      className='grid min-h-screen place-items-center overflow-x-hidden bg-[radial-gradient(120%_140%_at_88%_-10%,var(--ai-softer),transparent_55%),var(--bg)] px-[18px] py-[22px]'
    >
      <div className='flex w-full max-w-[448px] flex-col gap-[12px]'>
        <AppBrand
          className='justify-center text-center'
          compact
          mark={<span className='font-[700] leading-none'>AI</span>}
          subtitle='console · ng'
          title='AI Efficiency'
        />

        <Card className={cn('grid-paper w-full overflow-hidden border-[var(--ai-line)]', className)} variant='accent'>
          <SectionCardHeader className='px-[18px] pt-[18px]' title={title} description={description} />
          <CardContentStack className='border-border border-t px-[18px] py-[18px]'>
            {aside}
            {children}
          </CardContentStack>
          {actions ? (
            <ActionGroup className='border-border border-t px-[18px] py-[12px]' dataSlot='auth-surface-actions' layout='split'>
              {actions}
            </ActionGroup>
          ) : null}
        </Card>

        <p
          data-slot='auth-surface-caption'
          className='px-[6px] text-center text-[11px] leading-[1.45] text-[var(--ink-4)]'
        >
          Same-origin auth bridge. Local dev reuses the current online session through secure cookies.
        </p>
      </div>
    </main>
  )
}
