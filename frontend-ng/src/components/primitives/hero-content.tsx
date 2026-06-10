import type * as React from 'react'
import { CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'

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
    <CardContent
      className={cn('flex flex-col gap-5 p-6 lg:flex-row lg:items-center lg:justify-between', className)}
      data-slot='hero-content'
    >
      <div className='max-w-2xl' data-slot='hero-copy'>
        {badge}
        <h1 className='mt-4 font-semibold text-2xl tracking-tight md:text-3xl' data-slot='hero-title'>{title}</h1>
        {description ? <p className='mt-2 text-muted-foreground text-sm' data-slot='hero-description'>{description}</p> : null}
      </div>
      {action ? <div data-slot='hero-action'>{action}</div> : null}
    </CardContent>
  )
}
