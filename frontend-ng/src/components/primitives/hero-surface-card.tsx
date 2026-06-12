import type * as React from 'react'
import { Card } from '@/components/ui/card'

export function HeroSurfaceCard({
  children,
  ...props
}: React.ComponentProps<typeof Card>) {
  return (
    <Card data-slot='hero-surface-card' variant='accent' {...props}>
      <div data-slot='hero-surface-card-body'>
        {children}
      </div>
    </Card>
  )
}
