import { Checkbox } from '@/components/ui/checkbox'

export function DataGridCheckbox({
  ariaLabel,
  checked,
  onCheckedChange
}: {
  ariaLabel: string
  checked: boolean | 'indeterminate'
  onCheckedChange: (checked: boolean) => void
}) {
  return (
    <Checkbox
      aria-label={ariaLabel}
      checked={checked}
      onCheckedChange={(value) => onCheckedChange(value === true)}
    />
  )
}
