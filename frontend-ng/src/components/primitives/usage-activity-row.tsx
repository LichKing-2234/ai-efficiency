import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { ToolGlyph } from './tool-glyph'

const usageActivityContentClass = 'min-w-0 flex-1'
const usageActivityTitleClass = 'truncate font-semibold text-sm'
const usageActivityMetaClass = 'mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11.5px] text-muted-foreground'
const usageActivityAmountClass = 'hidden w-20 text-right tnum sm:block'

function UsageActivityContent({ children }: { children: React.ReactNode }) {
  return (
    <div className={usageActivityContentClass} data-slot='usage-activity-content'>
      {children}
    </div>
  )
}

function UsageActivityTitle({ children }: { children: React.ReactNode }) {
  return (
    <div className={usageActivityTitleClass} data-slot='usage-activity-title'>
      {children}
    </div>
  )
}

function UsageActivityMeta({
  endedAt,
  tokens
}: {
  endedAt: React.ReactNode
  tokens: React.ReactNode
}) {
  return (
    <div className={usageActivityMetaClass} data-slot='usage-activity-meta'>
      <span>{endedAt}</span>
      <span className='text-[var(--ink-4)]'>·</span>
      <span className='mono tnum'>{tokens}</span>
    </div>
  )
}

function UsageActivityAmount({
  credit,
  requests
}: {
  credit: React.ReactNode
  requests: React.ReactNode
}) {
  return (
    <div className={usageActivityAmountClass} data-slot='usage-activity-amount'>
      <div className='font-semibold text-sm'>{credit}</div>
      <div className='text-[11px] text-muted-foreground'>{requests}</div>
    </div>
  )
}

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
      <UsageActivityContent>
        <UsageActivityTitle>{title}</UsageActivityTitle>
        <UsageActivityMeta endedAt={endedAt} tokens={tokens} />
      </UsageActivityContent>
      <Badge variant={bound ? 'success' : 'warning'}>{statusLabel}</Badge>
      <UsageActivityAmount credit={credit} requests={requests} />
    </div>
  )
}
