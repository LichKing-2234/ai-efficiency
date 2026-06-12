import { Field, FieldLabel } from '@/components/ui/field'
import { LabeledSegmentedControl } from '@/components/primitives/labeled-segmented-control'
import type { SegmentedOption } from '@/components/primitives/segmented-control'

export function SegmentedField<T extends string>({
  ariaLabel,
  disabled,
  id,
  label,
  onChange,
  options,
  value
}: {
  ariaLabel: string
  disabled?: boolean
  id: string
  label: React.ReactNode
  onChange: (value: T) => void
  options: Array<SegmentedOption<T>>
  value: T
}) {
  return (
    <Field data-disabled={disabled ? true : undefined}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <LabeledSegmentedControl
        ariaLabel={ariaLabel}
        label={label}
        onChange={onChange}
        options={options}
        value={value}
      />
    </Field>
  )
}
