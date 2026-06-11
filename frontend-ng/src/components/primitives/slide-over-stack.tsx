import type * as React from 'react'
import { Stack } from '@/components/primitives/stack'

export function SlideOverStack({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <Stack className={className} dataSlot='slide-over-stack' gap='loose'>{children}</Stack>
  )
}
