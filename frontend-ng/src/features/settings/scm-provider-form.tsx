import { FormFieldGroup } from '@/components/primitives/form-field-group'
import { ManagedFormFooter } from '@/components/primitives/managed-form-footer'
import { SegmentedField } from '@/components/primitives/segmented-field'
import { SelectField } from '@/components/primitives/select-field'
import { TextField } from '@/components/primitives/text-field'
import type { Credential } from '@/lib/api/types'
import { useI18n } from '@/lib/i18n/i18n'
import type { ScmFormState } from './settings-payloads'

export function ScmProviderForm({
  createPending,
  credentials,
  editMode,
  errors = [],
  form,
  onCancel,
  onChange,
  onSubmit,
  updatePending
}: {
  createPending?: boolean
  credentials: Credential[]
  editMode?: boolean
  errors?: Array<string | undefined>
  form: ScmFormState
  onCancel: () => void
  onChange: (form: ScmFormState) => void
  onSubmit: () => void
  updatePending?: boolean
}) {
  const { t } = useI18n()
  const submitDisabled = !form.name || !form.base_url || !form.api_credential_id || createPending || updatePending

  return (
    <FormFieldGroup>
      <TextField id='scm-name' label={t('settings.name')} value={form.name} onChange={(name) => onChange({ ...form, name })} />
      <SegmentedField
        ariaLabel={t('settings.codePlatforms')}
        disabled={editMode}
        id='scm-platform-type'
        label={t('settings.codePlatforms')}
        onChange={(type) => {
          if (!editMode) onChange({ ...form, type })
        }}
        options={[
          { value: 'github', label: 'GitHub' },
          { value: 'bitbucket', label: 'Bitbucket' }
        ]}
        value={form.type}
      />
      <TextField id='scm-base-url' label={t('settings.baseUrl')} value={form.base_url} onChange={(base_url) => onChange({ ...form, base_url })} />
      <SelectField
        id='scm-api-credential'
        label={t('settings.apiCredential')}
        options={[
          { label: t('settings.apiCredential'), value: 'none' },
          ...credentials.map((credential) => ({ label: credential.name, value: String(credential.id) }))
        ]}
        triggerClassName='w-full'
        value={form.api_credential_id || 'none'}
        onValueChange={(value) => onChange({ ...form, api_credential_id: value === 'none' ? '' : value })}
      />
      <SegmentedField
        ariaLabel={t('settings.cloneHttps')}
        id='scm-clone-protocol'
        label={t('settings.cloneHttps')}
        onChange={(clone_protocol) => onChange({ ...form, clone_protocol })}
        options={[
          { value: 'https', label: t('settings.cloneHttps') },
          { value: 'ssh', label: t('settings.cloneSsh') }
        ]}
        value={form.clone_protocol}
      />
      {form.clone_protocol === 'ssh' ? (
        <>
          <TextField id='scm-ssh-host' label={t('settings.sshHost')} value={form.ssh_host} onChange={(ssh_host) => onChange({ ...form, ssh_host })} />
          <SelectField
            id='scm-clone-credential'
            label={t('settings.cloneCredential')}
            options={[
              { label: t('settings.cloneCredential'), value: 'none' },
              ...credentials.map((credential) => ({ label: credential.name, value: String(credential.id) }))
            ]}
            triggerClassName='w-full'
            value={form.clone_credential_id || 'none'}
            onValueChange={(value) => onChange({ ...form, clone_credential_id: value === 'none' ? '' : value })}
          />
        </>
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
