import type * as React from 'react'
import { AccentSurfaceCard } from '@/components/primitives/accent-surface-card'

export function HeroSurfaceCard({
  children,
  ...props
}: React.ComponentProps<typeof AccentSurfaceCard>) {
  return (
    <AccentSurfaceCard dataSlot='hero-surface-card' {...props}>
      <div data-slot='hero-surface-card-body'>
        {children}
      </div>
    </AccentSurfaceCard>
  )
}
