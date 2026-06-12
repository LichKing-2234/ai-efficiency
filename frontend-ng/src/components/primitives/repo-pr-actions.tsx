import { Button } from '@/components/ui/button'
import { FormActions } from '@/components/primitives/form-actions'

export function RepoPrActions({
  detailsDisabled = false,
  detailsLabel,
  expanded,
  onRefresh,
  onResolve,
  onToggleDetails,
  refreshDisabled = false,
  refreshLabel,
  resolveDisabled = false,
  resolveLabel
}: {
  detailsDisabled?: boolean
  detailsLabel?: string
  expanded?: boolean
  onRefresh: () => void
  onResolve?: () => void
  onToggleDetails?: () => void
  refreshDisabled?: boolean
  refreshLabel: string
  resolveDisabled?: boolean
  resolveLabel?: string
}) {
  return (
    <FormActions wrap>
      {onToggleDetails && detailsLabel ? (
        <Button variant='ghost' size='sm' onClick={onToggleDetails} disabled={detailsDisabled}>
          {detailsLabel}
        </Button>
      ) : null}
      <Button variant='outline' size='sm' onClick={onRefresh} disabled={refreshDisabled}>
        {refreshLabel}
      </Button>
      {onResolve && resolveLabel ? (
        <Button variant='outline' size='sm' onClick={onResolve} disabled={resolveDisabled}>
          {resolveLabel}
        </Button>
      ) : null}
    </FormActions>
  )
}
