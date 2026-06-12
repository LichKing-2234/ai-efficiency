import type * as React from 'react'
import { CountBadge } from '@/components/primitives/count-badge'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

export const lineTabClassName = 'h-8 gap-2 px-3'

export function LineTabs<T extends string>({
  ariaLabel,
  items,
  onChange,
  value
}: {
  ariaLabel?: string
  items: Array<{
    value: T
    label: React.ReactNode
    count?: React.ReactNode
  }>
  onChange: (value: T) => void
  value: T
}) {
  return (
    <Tabs aria-label={ariaLabel} onValueChange={(next) => onChange(next as T)} value={value}>
      <TabsList variant='line' wrap>
        {items.map((item) => (
          <TabsTrigger className={lineTabClassName} key={item.value} value={item.value}>
            {item.label}
            {item.count ? <CountBadge variant='secondary'>{item.count}</CountBadge> : null}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  )
}
