import type * as React from 'react'
import { CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'

const heroTitleClass = 'mt-4 font-semibold text-2xl tracking-tight md:text-3xl'
const heroDescriptionClass = 'mt-2 text-muted-foreground text-sm'

function HeroTitle({ children }: { children: React.ReactNode }) {
  return (
    <h1 className={heroTitleClass} data-slot='hero-title'>
      {children}
    </h1>
  )
}

function HeroDescription({ children }: { children: React.ReactNode }) {
  return (
    <p className={heroDescriptionClass} data-slot='hero-description'>
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
    <CardContent
      className={cn('flex flex-col gap-5 p-6 lg:flex-row lg:items-center lg:justify-between', className)}
      data-slot='hero-content'
    >
      <div className='max-w-2xl' data-slot='hero-copy'>
        {badge}
        <HeroTitle>{title}</HeroTitle>
        {description ? <HeroDescription>{description}</HeroDescription> : null}
      </div>
      {action ? <div data-slot='hero-action'>{action}</div> : null}
    </CardContent>
  )
}
