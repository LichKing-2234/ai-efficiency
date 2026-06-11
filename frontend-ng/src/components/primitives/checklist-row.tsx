import { CheckIcon, CircleIcon } from 'lucide-react'
import { ActionGroup } from './action-group'
import { FilterRow } from '@/components/primitives/filter-row'
import { cn } from '@/lib/utils'

export function ChecklistRow({
  action,
  className,
  label,
  ok,
  value
}: {
  action?: React.ReactNode
  className?: string
  label: React.ReactNode
  ok: boolean
  value: React.ReactNode
}) {
  const Icon = ok ? CheckIcon : CircleIcon

  return (
    <FilterRow
      justify='between'
      className={cn('rounded-[var(--r-sm)] border border-[var(--line-faint)] bg-[var(--surface-inset)] px-[11px] py-[9px] text-[12px]', className)}
      data-slot='checklist-row'
      data-state={ok ? 'ready' : 'pending'}
    >
      <ActionGroup align='start' className='min-w-0 text-[var(--ink-3)]' dataSlot='checklist-row-label' fit>
        <span className={cn('grid size-5 shrink-0 place-items-center rounded-full border [&_svg:not([class*=size-])]:size-3', ok ? 'border-[var(--pos-line)] bg-[var(--pos-soft)] text-[var(--pos)]' : 'border-[var(--warn-line)] bg-[var(--warn-soft)] text-[var(--warn)]')}>
          <Icon data-icon='inline-start' />
        </span>
        <span className='truncate text-[12.5px] font-medium'>{label}</span>
      </ActionGroup>
      <ActionGroup align='responsive-end' className='gap-2' dataSlot='checklist-row-actions'>
        <span
          className={cn(
            'shrink-0 text-[12px] font-medium',
            ok ? 'text-[var(--ink-3)]' : 'text-[var(--warn)]'
          )}
          data-slot='checklist-row-value'
        >
          {value}
        </span>
        {action}
      </ActionGroup>
    </FilterRow>
  )
}
