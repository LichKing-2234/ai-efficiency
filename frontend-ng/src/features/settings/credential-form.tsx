import { FormFieldGroup } from '@/components/primitives/form-field-group'
import { ManagedFormFooter } from '@/components/primitives/managed-form-footer'
import { SegmentedField } from '@/components/primitives/segmented-field'
import { TextField } from '@/components/primitives/text-field'
import { useI18n } from '@/lib/i18n/i18n'
import type { CredentialFormState } from './settings-payloads'

type CredentialKind = CredentialFormState['kind']

export function CredentialForm({
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
  form: CredentialFormState
  onCancel: () => void
  onChange: (form: CredentialFormState) => void
  onSubmit: () => void
  updatePending?: boolean
}) {
  const { t } = useI18n()
  const submitDisabled = !form.name ||
    (!editMode && form.kind === 'secret_text' && !form.text) ||
    (!editMode && form.kind === 'username_password' && (!form.username || !form.password)) ||
    (!editMode && form.kind === 'ssh_username_with_private_key' && (!form.username || !form.private_key)) ||
    createPending ||
    updatePending

  return (
    <FormFieldGroup>
      <TextField id='credential-name' label={t('settings.name')} value={form.name} onChange={(name) => onChange({ ...form, name })} />
      <TextField id='credential-description' label={t('settings.credentialDescription')} value={form.description ?? ''} onChange={(description) => onChange({ ...form, description })} />
      <SegmentedField
        ariaLabel={t('settings.addCredential')}
        id='credential-kind'
        label={t('settings.addCredential')}
        onChange={(kind: CredentialKind) => onChange({ ...form, kind })}
        options={[
          { value: 'secret_text', label: t('settings.secretTextKind') },
          { value: 'username_password', label: t('settings.usernamePasswordKind') },
          { value: 'ssh_username_with_private_key', label: t('settings.sshPrivateKeyKind') }
        ]}
        value={form.kind}
      />
      {form.kind === 'secret_text' ? (
        <TextField id='credential-secret-text' label={t('settings.secretText')} multiline value={form.text ?? ''} onChange={(text) => onChange({ ...form, text })} />
      ) : null}
      {form.kind !== 'secret_text' ? (
        <TextField id='credential-username' label={t('settings.username')} value={form.username ?? ''} onChange={(username) => onChange({ ...form, username })} />
      ) : null}
      {form.kind === 'username_password' ? (
        <TextField id='credential-password' label={t('settings.password')} type='password' value={form.password ?? ''} onChange={(password) => onChange({ ...form, password })} />
      ) : null}
      {form.kind === 'ssh_username_with_private_key' ? (
        <TextField id='credential-private-key' label={t('settings.privateKey')} multiline value={form.private_key ?? ''} onChange={(private_key) => onChange({ ...form, private_key })} />
      ) : null}
      {form.kind === 'ssh_username_with_private_key' ? (
        <TextField id='credential-passphrase' label={t('settings.passphrase')} type='password' value={form.passphrase ?? ''} onChange={(passphrase) => onChange({ ...form, passphrase })} />
      ) : null}
      <ManagedFormFooter
        cancelLabel={t('common.cancel')}
        errors={errors}
        submitDisabled={submitDisabled}
        submitLabel={editMode ? t('common.update') : t('common.create')}
        onCancel={onCancel}
        onSubmit={onSubmit}
      />
    </FormFieldGroup>
  )
}
