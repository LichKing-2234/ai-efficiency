import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ActionGroup } from '@/components/primitives/action-group'
import { SelectField } from '@/components/primitives/select-field'
import type {
  AdminAssignableSubscriptionGroup,
  AdminAssignableSubscriptionProvider,
  AdminSubscriptionManageOperation,
  AdminSubscriptionManageScope
} from '@/lib/api/types'

export interface AdminSubscriptionFormLabels {
  add: string
  allMapped: string
  confirm: string
  currentFilter: string
  days: string
  extend: string
  group: string
  jobRunning: string
  operation: string
  provider: string
  remove: string
  scope: string
  selectedUsers: (count: number) => string
  startJob: string
}

export function AdminSubscriptionForm({
  activeGroupId,
  activeGroups,
  activeJobRunning,
  activeProvider,
  canSubmit,
  confirmRemove,
  days,
  labels,
  onConfirmRemoveChange,
  onDaysChange,
  onGroupChange,
  onOperationChange,
  onProviderChange,
  onScopeChange,
  onStart,
  operation,
  scope,
  selectedCount,
  subscriptionProviders
}: {
  activeGroupId: string
  activeGroups: AdminAssignableSubscriptionGroup[]
  activeJobRunning: boolean
  activeProvider?: AdminAssignableSubscriptionProvider
  canSubmit: boolean
  confirmRemove: boolean
  days: number
  labels: AdminSubscriptionFormLabels
  onConfirmRemoveChange: (value: boolean) => void
  onDaysChange: (value: number) => void
  onGroupChange: (value: string) => void
  onOperationChange: (value: AdminSubscriptionManageOperation) => void
  onProviderChange: (value: string) => void
  onScopeChange: (value: AdminSubscriptionManageScope) => void
  onStart: () => void
  operation: AdminSubscriptionManageOperation
  scope: AdminSubscriptionManageScope
  selectedCount: number
  subscriptionProviders: AdminAssignableSubscriptionProvider[]
}) {
  return (
    <FieldGroup className='gap-3'>
      <div className='grid gap-3 md:grid-cols-[150px_150px_minmax(0,1fr)_minmax(0,1fr)_120px_auto]'>
        <SelectField
          disabled={activeJobRunning}
          id='admin-subscription-scope'
          label={labels.scope}
          options={[
            { label: labels.selectedUsers(selectedCount), value: 'selected' },
            { label: labels.currentFilter, value: 'current_filter' },
            { label: labels.allMapped, value: 'all_mapped' }
          ]}
          value={scope}
          onValueChange={(value) => onScopeChange(value as AdminSubscriptionManageScope)}
        />
        <SelectField
          disabled={activeJobRunning}
          id='admin-subscription-operation'
          label={labels.operation}
          options={[
            { label: labels.add, value: 'add' },
            { label: labels.extend, value: 'extend' },
            { label: labels.remove, value: 'remove' }
          ]}
          value={operation}
          onValueChange={(value) => onOperationChange(value as AdminSubscriptionManageOperation)}
        />
        <SelectField
          disabled={activeJobRunning}
          id='admin-subscription-provider'
          label={labels.provider}
          options={[
            { label: labels.provider, value: 'none' },
            ...subscriptionProviders.map((provider) => ({ label: provider.display_name || provider.name, value: String(provider.id) }))
          ]}
          value={activeProvider ? String(activeProvider.id) : 'none'}
          onValueChange={onProviderChange}
        />
        <SelectField
          disabled={activeJobRunning}
          id='admin-subscription-group'
          label={labels.group}
          options={[
            { label: labels.group, value: 'none' },
            ...activeGroups.map((group) => ({ label: `${group.group_name} · ${group.platform}`, value: group.group_id }))
          ]}
          value={activeGroupId || 'none'}
          onValueChange={onGroupChange}
        />
        {operation !== 'remove' ? (
          <Field>
            <FieldLabel htmlFor='admin-subscription-days'>{labels.days}</FieldLabel>
            <Input
              id='admin-subscription-days'
              type='number'
              min={1}
              value={String(days)}
              disabled={activeJobRunning}
              onChange={(event) => onDaysChange(Number(event.target.value) || 0)}
            />
          </Field>
        ) : (
          <Field orientation='horizontal' className='min-h-14 items-end pb-1'>
            <Checkbox checked={confirmRemove} disabled={activeJobRunning} onCheckedChange={(value) => onConfirmRemoveChange(value === true)} />
            <FieldLabel>{labels.confirm}</FieldLabel>
          </Field>
        )}
        <ActionGroup className='items-end'>
          <Button variant='outline' disabled={!canSubmit} onClick={onStart}>
            {activeJobRunning ? labels.jobRunning : labels.startJob}
          </Button>
        </ActionGroup>
      </div>
    </FieldGroup>
  )
}
