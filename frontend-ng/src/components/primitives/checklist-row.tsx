import { CheckIcon, CircleIcon } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
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
    <div
      className={cn('flex items-center justify-between gap-3 rounded-[var(--r-sm)] border border-[var(--line-faint)] bg-[var(--surface-inset)] px-3 py-2 text-sm', className)}
      data-slot='checklist-row'
      data-state={ok ? 'ready' : 'pending'}
    >
      <span className='flex min-w-0 items-center gap-2 text-muted-foreground'>
        <span className={cn('grid size-5 shrink-0 place-items-center rounded-full border [&_svg:not([class*=size-])]:size-3', ok ? 'border-[var(--pos-line)] bg-[var(--pos-soft)] text-[var(--pos)]' : 'border-[var(--warn-line)] bg-[var(--warn-soft)] text-[var(--warn)]')}>
          <Icon data-icon='inline-start' />
        </span>
        <span className='truncate'>{label}</span>
      </span>
      <span className='flex shrink-0 items-center gap-2'>
        <Badge variant={ok ? 'success' : 'warning'}>{value}</Badge>
        {action}
      </span>
    </div>
  )
}
