import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ActionGroup } from '@/components/primitives/action-group'
import { AppAlert } from '@/components/primitives/app-alert'
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
        <Field data-disabled={field.key === 'name' && editMode ? true : undefined} key={field.key}>
          <FieldLabel htmlFor={field.id}>{t(field.labelKey)}</FieldLabel>
          <Input
            disabled={field.key === 'name' && editMode}
            id={field.id}
            type={field.type}
            value={form[field.key]}
            onChange={(event) => onChange({ ...form, [field.key]: event.target.value })}
          />
        </Field>
      ))}
      <Field orientation='horizontal'>
        <Checkbox
          id='relay-primary'
          checked={form.is_primary}
          onCheckedChange={(checked) => onChange({ ...form, is_primary: checked === true })}
        />
        <FieldLabel htmlFor='relay-primary'>{t('settings.primary')}</FieldLabel>
      </Field>
      <Field orientation='horizontal'>
        <Checkbox
          id='relay-enabled'
          checked={form.enabled}
          onCheckedChange={(checked) => onChange({ ...form, enabled: checked === true })}
        />
        <FieldLabel htmlFor='relay-enabled'>{t('settings.enabled')}</FieldLabel>
      </Field>
      {errors.filter((message): message is string => !!message).map((message) => (
        <AppAlert key={message} tone='error' title={message} />
      ))}
      <ActionGroup>
        <Button variant='outline' onClick={onCancel}>{t('common.cancel')}</Button>
        <Button
          disabled={submitDisabled}
          onClick={onSubmit}
        >
          {editMode ? t('common.update') : t('common.create')}
        </Button>
      </ActionGroup>
    </FieldGroup>
  )
}
