import type * as React from 'react'
import { cn } from '@/lib/utils'
import { LabeledSegmentedControl } from '@/components/primitives/labeled-segmented-control'
import type { SegmentedOption } from '@/components/primitives/segmented-control'

export function FilterSegmentedControl<T extends string>({
  ariaLabel,
  className,
  label,
  onChange,
  options,
  value
}: {
  ariaLabel: string
  className?: string
  label: React.ReactNode
  onChange: (value: T) => void
  options: Array<SegmentedOption<T>>
  value: T
}) {
  return (
    <LabeledSegmentedControl
      ariaLabel={ariaLabel}
      className={cn('shrink-0', className)}
      label={label}
      onChange={onChange}
      options={options}
      value={value}
    />
  )
}
