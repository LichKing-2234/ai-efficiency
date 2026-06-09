import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { AdminSubscriptionForm } from './admin-subscription-form'
import type { AdminAssignableSubscriptionProvider } from '@/lib/api/types'

const providers: AdminAssignableSubscriptionProvider[] = [
  {
    id: 2,
    name: 'relay-main',
    display_name: 'Relay Main',
    groups: [
      { group_id: 'group-a', group_name: 'Group Alpha', platform: 'claude', subscription_type: 'pro' },
      { group_id: 'group-b', group_name: 'Group Beta', platform: 'codex', subscription_type: 'pro' }
    ]
  }
]

describe('AdminSubscriptionForm', () => {
  test('renders subscription controls through shadcn field primitives', () => {
    const html = renderToStaticMarkup(
      <AdminSubscriptionForm
        activeGroupId='group-a'
        activeJobRunning={false}
        activeProvider={providers[0]}
        activeGroups={providers[0].groups}
        canSubmit
        confirmRemove={false}
        days={30}
        labels={labels()}
        operation='add'
        scope='selected'
        selectedCount={2}
        subscriptionProviders={providers}
        onConfirmRemoveChange={() => undefined}
        onDaysChange={() => undefined}
        onGroupChange={() => undefined}
        onOperationChange={() => undefined}
        onProviderChange={() => undefined}
        onStart={() => undefined}
        onScopeChange={() => undefined}
      />
    )

    expect(html).toContain('data-slot="field-group"')
    expect(html).toContain('for="admin-subscription-scope"')
    expect(html).toContain('for="admin-subscription-operation"')
    expect(html).toContain('for="admin-subscription-provider"')
    expect(html).toContain('for="admin-subscription-group"')
    expect(html).toContain('for="admin-subscription-days"')
    expect(html).toContain('data-slot="action-group"')
  })

  test('renders remove confirmation as a field instead of the days input', () => {
    const html = renderToStaticMarkup(
      <AdminSubscriptionForm
        activeGroupId='group-a'
        activeJobRunning={false}
        activeProvider={providers[0]}
        activeGroups={providers[0].groups}
        canSubmit={false}
        confirmRemove
        days={0}
        labels={labels()}
        operation='remove'
        scope='all_mapped'
        selectedCount={0}
        subscriptionProviders={providers}
        onConfirmRemoveChange={() => undefined}
        onDaysChange={() => undefined}
        onGroupChange={() => undefined}
        onOperationChange={() => undefined}
        onProviderChange={() => undefined}
        onStart={() => undefined}
        onScopeChange={() => undefined}
      />
    )

    expect(html).toContain('Confirm')
    expect(html).not.toContain('id="admin-subscription-days"')
  })
})

function labels() {
  return {
    add: 'Add',
    allMapped: 'All mapped',
    confirm: 'Confirm',
    currentFilter: 'Current filter',
    days: 'Days',
    extend: 'Extend',
    group: 'Group',
    jobRunning: 'Job running',
    operation: 'Operation',
    provider: 'Provider',
    remove: 'Remove',
    scope: 'Scope',
    selectedUsers: (count: number) => `Selected users (${count})`,
    startJob: 'Start job'
  }
}
