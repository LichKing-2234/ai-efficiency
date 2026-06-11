import { cn } from '@/lib/utils'
import { ActionGroup } from './action-group'
import { SegmentedControl, type SegmentedOption } from './segmented-control'

export function LabeledSegmentedControl<T extends string>({
  ariaLabel,
  className,
  label,
  onChange,
  options,
  size = 'sm',
  value
}: {
  ariaLabel: string
  className?: string
  label: React.ReactNode
  onChange: (value: T) => void
  options: Array<SegmentedOption<T>>
  size?: 'default' | 'sm'
  value: T
}) {
  return (
    <ActionGroup align='start' className={className} dataSlot='labeled-segmented-control'>
      <span className='font-semibold text-[11.5px] text-[var(--ink-4)]' data-slot='labeled-segmented-control-label'>
        {label}
      </span>
      <SegmentedControl ariaLabel={ariaLabel} onChange={onChange} options={options} size={size} value={value} />
    </ActionGroup>
  )
}
