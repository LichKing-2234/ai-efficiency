import type * as React from 'react'
import { Card } from '@/components/ui/card'
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
    <Card className={cn('grid-paper w-full overflow-hidden border-[var(--ai-line)]', className)} data-slot='auth-surface-frame' variant='accent'>
      <SectionCardHeader className='px-[18px] pt-[18px]' title={title} description={description} />
      <CardContentStack className='border-border border-t px-[18px] py-[18px]'>
        {aside}
        {children}
      </CardContentStack>
    </Card>
  )
}
