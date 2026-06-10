import { Stack } from '@/components/primitives/stack'
import { cn } from '@/lib/utils'

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
    <div className={cn('flex flex-col gap-3', className)} data-slot='token-breakdown'>
      <div className='flex h-2.5 overflow-hidden rounded-full bg-[var(--surface-inset)]' data-slot='token-breakdown-bar'>
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
      </div>
      <Stack gap='compact'>
        {items.map((item) => (
          <div className='flex items-center gap-2 text-[12.5px]' data-slot='token-breakdown-row' key={String(item.label)}>
            <span className='size-2.5 rounded-sm' style={{ background: item.color }} />
            <span className='min-w-0 flex-1 truncate text-[var(--ink-2)]'>{item.label}</span>
            <span className='mono tnum shrink-0 font-semibold'>{valueFormatter(item.value)}</span>
          </div>
        ))}
      </Stack>
    </div>
  )
}
