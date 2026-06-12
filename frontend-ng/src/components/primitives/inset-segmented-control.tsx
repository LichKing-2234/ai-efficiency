import type * as React from 'react'
import { LabeledSegmentedControl } from '@/components/primitives/labeled-segmented-control'
import type { SegmentedOption } from '@/components/primitives/segmented-control'

export function InsetSegmentedControl<T extends string>({
  ariaLabel,
  label,
  onChange,
  options,
  value
}: {
  ariaLabel: string
  label: React.ReactNode
  onChange: (value: T) => void
  options: Array<SegmentedOption<T>>
  value: T
}) {
  return (
    <LabeledSegmentedControl
      ariaLabel={ariaLabel}
      className='border-[var(--line-faint)] border-b px-[12px] py-[9px] last:border-b-0'
      label={label}
      onChange={onChange}
      options={options}
      value={value}
    />
  )
}
