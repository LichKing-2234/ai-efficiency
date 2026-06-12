import { ToolbarSelect } from '@/components/primitives/toolbar-select'

export function PageSizeSelect({
  ariaLabel,
  labelMode = 'labeled',
  onValueChange,
  size,
  sizes = [20, 50, 100],
  tPageSize,
  value,
  width = 'compact'
}: {
  ariaLabel: string
  labelMode?: 'labeled' | 'plain'
  onValueChange: (value: number) => void
  size?: 'default' | 'sm'
  sizes?: number[]
  tPageSize?: (size: number) => string
  value: number
  width?: 'auto' | 'compact' | 'full' | 'toolbar'
}) {
  return (
    <ToolbarSelect
      ariaLabel={ariaLabel}
      options={sizes.map((sizeValue) => ({
        value: String(sizeValue),
        label: labelMode === 'plain'
          ? String(sizeValue)
          : tPageSize
            ? tPageSize(sizeValue)
            : String(sizeValue)
      }))}
      size={size}
      value={String(value)}
      width={width}
      onValueChange={(nextValue) => onValueChange(Number(nextValue))}
    />
  )
}
