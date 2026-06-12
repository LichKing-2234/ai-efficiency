import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { AdminSubscriptionForm } from './admin-subscription-form'
import type { AdminAssignableSubscriptionProvider } from '@/lib/api/types'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'admin-subscription-form.tsx'), 'utf8')

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
    expect(html).toContain('data-slot="block-end-actions"')
  })

  test('uses compact field group rhythm without local gap classes', () => {
    expect(source).toContain("from '@/components/primitives/form-field-group'")
    expect(source).toContain("<FormFieldGroup gap='compact'>")
    expect(source).not.toContain("from '@/components/ui/field'")
    expect(source).not.toContain("<FieldGroup className='gap-3'>")
  })

  test('uses shared block-end action alignment for the start job control', () => {
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

    expect(html).toContain('data-slot="block-end-actions"')
    expect(source).toContain("from '@/components/primitives/block-end-actions'")
    expect(source).toContain("from '@/components/primitives/secondary-action-button'")
    expect(source).toContain('<BlockEndActions>')
    expect(source).toContain('<SecondaryActionButton')
    expect(source).not.toContain("<ActionGroup align='block-end'>")
    expect(source).not.toContain("<ActionGroup className='items-end'>")
    expect(html).toContain('items-end')
    expect(html).not.toContain('class="flex items-center gap-2 justify-end items-end"')
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

  test('uses shared checkbox alignment for the remove confirmation control', () => {
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

    expect(html).toContain('data-align="block-end"')
    expect(source).toContain("align='block-end'")
    expect(source).not.toContain("className='min-h-14 items-end pb-1'")
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
