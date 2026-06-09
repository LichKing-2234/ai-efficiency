import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { ActionGroup } from '@/components/primitives/action-group'
import { AppAlert } from '@/components/primitives/app-alert'
import { LabeledSegmentedControl } from '@/components/primitives/labeled-segmented-control'
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
    <FieldGroup>
      <Field>
        <FieldLabel htmlFor='credential-name'>{t('settings.name')}</FieldLabel>
        <Input
          id='credential-name'
          value={form.name}
          onChange={(event) => onChange({ ...form, name: event.target.value })}
        />
      </Field>
      <Field>
        <FieldLabel htmlFor='credential-description'>{t('settings.credentialDescription')}</FieldLabel>
        <Input
          id='credential-description'
          value={form.description}
          onChange={(event) => onChange({ ...form, description: event.target.value })}
        />
      </Field>
      <Field>
        <FieldLabel>{t('settings.addCredential')}</FieldLabel>
        <LabeledSegmentedControl
          ariaLabel={t('settings.addCredential')}
          label={t('settings.addCredential')}
          onChange={(kind: CredentialKind) => onChange({ ...form, kind })}
          options={[
            { value: 'secret_text', label: t('settings.secretTextKind') },
            { value: 'username_password', label: t('settings.usernamePasswordKind') },
            { value: 'ssh_username_with_private_key', label: t('settings.sshPrivateKeyKind') }
          ]}
          value={form.kind}
        />
      </Field>
      {form.kind === 'secret_text' ? (
        <Field>
          <FieldLabel htmlFor='credential-secret-text'>{t('settings.secretText')}</FieldLabel>
          <Textarea
            id='credential-secret-text'
            value={form.text}
            onChange={(event) => onChange({ ...form, text: event.target.value })}
          />
        </Field>
      ) : null}
      {form.kind !== 'secret_text' ? (
        <Field>
          <FieldLabel htmlFor='credential-username'>{t('settings.username')}</FieldLabel>
          <Input
            id='credential-username'
            value={form.username}
            onChange={(event) => onChange({ ...form, username: event.target.value })}
          />
        </Field>
      ) : null}
      {form.kind === 'username_password' ? (
        <Field>
          <FieldLabel htmlFor='credential-password'>{t('settings.password')}</FieldLabel>
          <Input
            id='credential-password'
            type='password'
            value={form.password}
            onChange={(event) => onChange({ ...form, password: event.target.value })}
          />
        </Field>
      ) : null}
      {form.kind === 'ssh_username_with_private_key' ? (
        <Field>
          <FieldLabel htmlFor='credential-private-key'>{t('settings.privateKey')}</FieldLabel>
          <Textarea
            id='credential-private-key'
            value={form.private_key}
            onChange={(event) => onChange({ ...form, private_key: event.target.value })}
          />
        </Field>
      ) : null}
      {form.kind === 'ssh_username_with_private_key' ? (
        <Field>
          <FieldLabel htmlFor='credential-passphrase'>{t('settings.passphrase')}</FieldLabel>
          <Input
            id='credential-passphrase'
            type='password'
            value={form.passphrase}
            onChange={(event) => onChange({ ...form, passphrase: event.target.value })}
          />
        </Field>
      ) : null}
      {errors.filter((message): message is string => !!message).map((message) => (
        <AppAlert key={message} tone='error' title={message} />
      ))}
      <ActionGroup>
        <Button variant='outline' onClick={onCancel}>{t('common.cancel')}</Button>
        <Button disabled={submitDisabled} onClick={onSubmit}>
          {editMode ? t('common.update') : t('common.create')}
        </Button>
      </ActionGroup>
    </FieldGroup>
  )
}
