import type * as React from 'react'
import { Stack } from '@/components/primitives/stack'
import { SectionEyebrow } from '@/components/primitives/section-eyebrow'

export function DetailSection({
  children,
  title
}: {
  children: React.ReactNode
  title: React.ReactNode
}) {
  return (
    <Stack dataSlot='detail-section' gap='compact'>
      <SectionEyebrow>{title}</SectionEyebrow>
      {children}
    </Stack>
  )
}
