import type * as React from 'react'
import { CardContentStack } from '@/components/primitives/card-content-stack'

export function PulseStripPanel({
  children
}: {
  children: React.ReactNode
}) {
  return (
    <CardContentStack className='border-border border-t px-[18px] py-4'>
      <div data-slot='pulse-strip-panel'>
        {children}
      </div>
    </CardContentStack>
  )
}
