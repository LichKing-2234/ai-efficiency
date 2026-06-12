import type * as React from 'react'
import { FramedCard } from '@/components/primitives/framed-card'

export function SearchWorkbenchCard({
  children,
  ...props
}: React.ComponentProps<typeof FramedCard>) {
  return (
    <FramedCard {...props}>
      <div data-slot='search-workbench-card'>
        {children}
      </div>
    </FramedCard>
  )
}
