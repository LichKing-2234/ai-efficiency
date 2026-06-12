import type * as React from 'react'
import { CardContentStack } from '@/components/primitives/card-content-stack'

export function ChartFooterPanel({
  children,
  label
}: {
  children: React.ReactNode
  label: React.ReactNode
}) {
  return (
    <CardContentStack className='border-[var(--line-faint)] border-t px-0 pt-3' dataSlot='chart-footer-panel'>
      <div className='text-[11.5px] font-medium text-[var(--ink-3)]' data-slot='chart-footer-panel-label'>
        {label}
      </div>
      {children}
    </CardContentStack>
  )
}
