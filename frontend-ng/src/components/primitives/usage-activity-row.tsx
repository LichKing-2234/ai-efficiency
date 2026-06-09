import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { ToolGlyph } from './tool-glyph'

export function UsageActivityRow({
  bound,
  className,
  credit,
  endedAt,
  first = false,
  requests,
  statusLabel,
  title,
  tokens,
  tool
}: {
  bound: boolean
  className?: string
  credit: React.ReactNode
  endedAt: React.ReactNode
  first?: boolean
  requests: React.ReactNode
  statusLabel: React.ReactNode
  title: React.ReactNode
  tokens: React.ReactNode
  tool?: string | null
}) {
  return (
    <div
      className={cn('flex items-center gap-3 py-3', !first && 'border-t border-[var(--line-faint)]', className)}
      data-slot='usage-activity-row'
      data-state={bound ? 'bound' : 'unbound'}
    >
      <ToolGlyph tool={tool} size={28} />
      <div className='min-w-0 flex-1'>
        <div className='truncate font-semibold text-sm'>{title}</div>
        <div className='mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11.5px] text-muted-foreground'>
          <span>{endedAt}</span>
          <span className='text-[var(--ink-4)]'>·</span>
          <span className='mono tnum'>{tokens}</span>
        </div>
      </div>
      <Badge variant={bound ? 'success' : 'warning'}>{statusLabel}</Badge>
      <div className='hidden w-20 text-right tnum sm:block'>
        <div className='font-semibold text-sm'>{credit}</div>
        <div className='text-[11px] text-muted-foreground'>{requests}</div>
      </div>
    </div>
  )
}
