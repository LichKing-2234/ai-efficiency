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
      className='grid min-h-screen place-items-center overflow-x-hidden bg-[radial-gradient(120%_140%_at_88%_-10%,var(--ai-softer),transparent_55%),var(--bg)] px-[18px] py-[22px]'
    >
      <div className='flex w-full max-w-[448px] flex-col gap-[12px]'>
        <div
          data-slot='auth-surface-brand'
          className='flex items-center justify-center gap-[10px] text-center'
        >
          <div
            data-slot='auth-surface-brand-mark'
            className='grid size-8 place-items-center rounded-[8px] bg-[linear-gradient(135deg,var(--ai-bright),var(--ai-deep))] text-white shadow-[0_2px_8px_var(--ai-glow)]'
          >
            <span className='text-[15px] font-[700] leading-none'>AI</span>
          </div>
          <div className='min-w-0 leading-[1.05]'>
            <div className='text-[13.5px] font-[650] tracking-[0] text-foreground'>AI Efficiency</div>
            <div className='mono mt-[2px] text-[10px] tracking-[0.02em] text-[var(--ink-4)]'>console · ng</div>
          </div>
        </div>

        <Card className={cn('grid-paper w-full overflow-hidden border-[var(--ai-line)] shadow-[var(--sh-lg)]', className)} variant='accent'>
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
