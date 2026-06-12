import type * as React from 'react'
import { AccentSurfaceCard } from '@/components/primitives/accent-surface-card'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { SectionCardHeader } from '@/components/primitives/section-card-header'
import { cn } from '@/lib/utils'

export function AuthSurfaceFrame({
  aside,
  children,
  className,
  description,
  title
}: {
  aside?: React.ReactNode
  children: React.ReactNode
  className?: string
  description: string
  title: string
}) {
  return (
    <AccentSurfaceCard className={cn('w-full', className)} dataSlot='auth-surface-frame'>
      <SectionCardHeader className='px-[18px] pt-[18px]' title={title} description={description} />
      <CardContentStack className='border-border border-t px-[18px] py-[18px]'>
        {aside}
        {children}
      </CardContentStack>
    </AccentSurfaceCard>
  )
}
