import { ActionGroup } from './action-group'
import { Stack } from '@/components/primitives/stack'

export type TokenBreakdownItem = {
  label: React.ReactNode
  value: number
  color: string
}

export function TokenBreakdown({
  className,
  items,
  valueFormatter
}: {
  className?: string
  items: TokenBreakdownItem[]
  valueFormatter: (value: number) => React.ReactNode
}) {
  const total = items.reduce((sum, item) => sum + Math.max(0, item.value), 0)

  return (
    <Stack className={className} dataSlot='token-breakdown' gap='compact'>
      <ActionGroup className='h-2.5 overflow-hidden rounded-full bg-[var(--surface-inset)] gap-0' dataSlot='token-breakdown-bar'>
        {items.map((item) => {
          const value = Math.max(0, item.value)
          const width = total > 0 ? (value / total) * 100 : 0
          return (
            <span
              data-slot='token-breakdown-segment'
              key={String(item.label)}
              style={{ width: `${width}%`, background: item.color }}
              title={typeof item.label === 'string' ? item.label : undefined}
            />
          )
        })}
      </ActionGroup>
      <Stack gap='compact'>
        {items.map((item) => (
          <ActionGroup align='start' className='text-[12.5px]' dataSlot='token-breakdown-row' fit key={String(item.label)}>
            <span className='size-2.5 rounded-sm' style={{ background: item.color }} />
            <span className='min-w-0 flex-1 truncate text-[var(--ink-2)]'>{item.label}</span>
            <span className='mono tnum shrink-0 font-semibold'>{valueFormatter(item.value)}</span>
          </ActionGroup>
        ))}
      </Stack>
    </Stack>
  )
}
