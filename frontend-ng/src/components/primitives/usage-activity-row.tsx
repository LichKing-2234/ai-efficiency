import { ActionGroup } from '@/components/primitives/action-group'
import { FilterRow } from '@/components/primitives/filter-row'
import { Stack } from '@/components/primitives/stack'
import { StatusBadge } from '@/components/primitives/status-badge'
import { cn } from '@/lib/utils'
import { ToolGlyph } from './tool-glyph'

function UsageActivityContent({ children }: { children: React.ReactNode }) {
  return (
    <Stack className='min-w-0 flex-1' dataSlot='usage-activity-content' gap='none'>
      {children}
    </Stack>
  )
}

function UsageActivityTitle({ children }: { children: React.ReactNode }) {
  return (
    <span className='block truncate text-[13px] font-[550]' data-slot='usage-activity-title'>
      {children}
    </span>
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
    <FilterRow className='mt-0.5 gap-x-2 gap-y-1 text-[11.5px] text-[var(--ink-3)]' dataSlot='usage-activity-meta'>
      <span>{endedAt}</span>
      <span className='text-[var(--ink-4)]'>·</span>
      <span className='mono tnum'>{tokens}</span>
    </FilterRow>
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
    <Stack className='hidden w-[88px] text-right tnum sm:block' dataSlot='usage-activity-amount' gap='none'>
      <div className='font-semibold text-[13px]'>{credit}</div>
      <div className='text-[11px] text-[var(--ink-3)]'>{requests}</div>
    </Stack>
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
  statusLabel: string
  title: React.ReactNode
  tokens: React.ReactNode
  tool?: string | null
}) {
  return (
    <ActionGroup
      align='start'
      className={cn('flex items-center gap-3 px-1 py-[11px]', !first && 'border-t border-[var(--line-faint)]', className)}
      dataSlot='usage-activity-row'
      data-state={bound ? 'bound' : 'unbound'}
      fit
    >
      <ToolGlyph tool={tool} size={28} />
      <UsageActivityContent>
        <UsageActivityTitle>{title}</UsageActivityTitle>
        <UsageActivityMeta endedAt={endedAt} tokens={tokens} />
      </UsageActivityContent>
      <StatusBadge label={statusLabel} value={bound ? 'bound' : 'unbound'} />
      <UsageActivityAmount credit={credit} requests={requests} />
    </ActionGroup>
  )
}
