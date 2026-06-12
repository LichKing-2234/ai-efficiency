import { FieldGroup } from '@/components/ui/field'
import { CheckboxField } from '@/components/primitives/checkbox-field'
import { ManagedFormFooter } from '@/components/primitives/managed-form-footer'
import { TextField } from '@/components/primitives/text-field'
import { useI18n } from '@/lib/i18n/i18n'
import type { RelayFormState } from './settings-payloads'

type RelayTextField = keyof Pick<RelayFormState, 'name' | 'display_name' | 'base_url' | 'admin_api_key'>

const relayFields: Array<{
  id: string
  key: RelayTextField
  labelKey: Parameters<ReturnType<typeof useI18n>['t']>[0]
  type?: React.HTMLInputTypeAttribute
}> = [
  { id: 'relay-name', key: 'name', labelKey: 'settings.name' },
  { id: 'relay-display-name', key: 'display_name', labelKey: 'settings.displayName' },
  { id: 'relay-base-url', key: 'base_url', labelKey: 'settings.baseUrl' },
  { id: 'relay-admin-api-key', key: 'admin_api_key', labelKey: 'settings.adminApiKey', type: 'password' }
]

export function RelayProviderForm({
  createPending,
  editMode,
  errors = [],
  form,
  onCancel,
  onChange,
  onSubmit,
  updatePending
}: {
  createPending?: boolean
  editMode?: boolean
  errors?: Array<string | undefined>
  form: RelayFormState
  onCancel: () => void
  onChange: (form: RelayFormState) => void
  onSubmit: () => void
  updatePending?: boolean
}) {
  const { t } = useI18n()
  const submitDisabled = !form.name || !form.display_name || !form.base_url || (!editMode && !form.admin_api_key) || createPending || updatePending

  return (
    <FieldGroup>
      {relayFields.map((field) => (
        <TextField
          disabled={field.key === 'name' && editMode}
          id={field.id}
          key={field.key}
          label={t(field.labelKey)}
          type={field.type}
          value={form[field.key]}
          onChange={(value) => onChange({ ...form, [field.key]: value })}
        />
      ))}
      <CheckboxField id='relay-primary' checked={form.is_primary} label={t('settings.primary')} onCheckedChange={(is_primary) => onChange({ ...form, is_primary })} />
      <CheckboxField id='relay-enabled' checked={form.enabled} label={t('settings.enabled')} onCheckedChange={(enabled) => onChange({ ...form, enabled })} />
      <ManagedFormFooter
        cancelLabel={t('common.cancel')}
        errors={errors}
        submitDisabled={submitDisabled}
        submitLabel={editMode ? t('common.update') : t('common.create')}
        onCancel={onCancel}
        onSubmit={onSubmit}
      />
    </FieldGroup>
  )
}
