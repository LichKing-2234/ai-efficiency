import type * as React from 'react'
import { InsetPanel } from '@/components/primitives/inset-panel'
import { cn } from '@/lib/utils'

const indentClasses = {
  none: '',
  selection: 'ml-11'
}

const maxWidthClasses = {
  none: '',
  xl: 'max-w-xl'
}

export function RowInsetPanel({
  children,
  className,
  columns = 7,
  indent = 'none',
  maxWidth = 'none'
}: {
  children: React.ReactNode
  className?: string
  columns?: number
  indent?: keyof typeof indentClasses
  maxWidth?: keyof typeof maxWidthClasses
}) {
  return (
    <InsetPanel
      className={cn(
        'flex flex-col gap-2 text-left text-xs',
        columns === 7 && 'col-span-7',
        indentClasses[indent],
        maxWidthClasses[maxWidth],
        className
      )}
      dataSlot='row-inset-panel'
    >
      {children}
    </InsetPanel>
  )
}
