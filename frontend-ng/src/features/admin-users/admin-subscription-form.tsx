import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ActionGroup } from '@/components/primitives/action-group'
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
        <Field>
          <FieldLabel htmlFor='admin-subscription-scope'>{labels.scope}</FieldLabel>
          <Select value={scope} disabled={activeJobRunning} onValueChange={(value) => onScopeChange(value as AdminSubscriptionManageScope)}>
            <SelectTrigger id='admin-subscription-scope'><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value='selected'>{labels.selectedUsers(selectedCount)}</SelectItem>
                <SelectItem value='current_filter'>{labels.currentFilter}</SelectItem>
                <SelectItem value='all_mapped'>{labels.allMapped}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
        <Field>
          <FieldLabel htmlFor='admin-subscription-operation'>{labels.operation}</FieldLabel>
          <Select value={operation} disabled={activeJobRunning} onValueChange={(value) => onOperationChange(value as AdminSubscriptionManageOperation)}>
            <SelectTrigger id='admin-subscription-operation'><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value='add'>{labels.add}</SelectItem>
                <SelectItem value='extend'>{labels.extend}</SelectItem>
                <SelectItem value='remove'>{labels.remove}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
        <Field>
          <FieldLabel htmlFor='admin-subscription-provider'>{labels.provider}</FieldLabel>
          <Select value={activeProvider ? String(activeProvider.id) : 'none'} disabled={activeJobRunning} onValueChange={onProviderChange}>
            <SelectTrigger id='admin-subscription-provider'><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value='none'>{labels.provider}</SelectItem>
                {subscriptionProviders.map((provider) => <SelectItem key={provider.id} value={String(provider.id)}>{provider.display_name || provider.name}</SelectItem>)}
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
        <Field>
          <FieldLabel htmlFor='admin-subscription-group'>{labels.group}</FieldLabel>
          <Select value={activeGroupId || 'none'} disabled={activeJobRunning} onValueChange={onGroupChange}>
            <SelectTrigger id='admin-subscription-group'><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value='none'>{labels.group}</SelectItem>
                {activeGroups.map((group) => <SelectItem key={group.group_id} value={group.group_id}>{group.group_name} · {group.platform}</SelectItem>)}
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
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
