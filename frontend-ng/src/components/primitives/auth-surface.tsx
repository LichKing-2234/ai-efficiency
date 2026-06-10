import type * as React from 'react'
import { Card } from '@/components/ui/card'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { SectionCardHeader } from '@/components/primitives/section-card-header'
import { cn } from '@/lib/utils'

export function AuthSurface({
  children,
  className,
  description,
  title
}: {
  children: React.ReactNode
  className?: string
  description: string
  title: string
}) {
  return (
    <main data-slot='auth-surface' className='grid min-h-screen place-items-center bg-background p-4'>
      <Card className={cn('w-full max-w-md', className)}>
        <SectionCardHeader title={title} description={description} />
        <CardContentStack>{children}</CardContentStack>
      </Card>
    </main>
  )
}
