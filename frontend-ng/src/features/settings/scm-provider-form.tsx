import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ActionGroup } from '@/components/primitives/action-group'
import { AppAlert } from '@/components/primitives/app-alert'
import { LabeledSegmentedControl } from '@/components/primitives/labeled-segmented-control'
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
    <FieldGroup>
      <Field>
        <FieldLabel htmlFor='scm-name'>{t('settings.name')}</FieldLabel>
        <Input
          id='scm-name'
          value={form.name}
          onChange={(event) => onChange({ ...form, name: event.target.value })}
        />
      </Field>
      <Field data-disabled={editMode ? true : undefined}>
        <FieldLabel>{t('settings.codePlatforms')}</FieldLabel>
        <LabeledSegmentedControl
          ariaLabel={t('settings.codePlatforms')}
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
      </Field>
      <Field>
        <FieldLabel htmlFor='scm-base-url'>{t('settings.baseUrl')}</FieldLabel>
        <Input
          id='scm-base-url'
          value={form.base_url}
          onChange={(event) => onChange({ ...form, base_url: event.target.value })}
        />
      </Field>
      <Field>
        <FieldLabel>{t('settings.apiCredential')}</FieldLabel>
        <Select value={form.api_credential_id || 'none'} onValueChange={(value) => onChange({ ...form, api_credential_id: value === 'none' ? '' : value })}>
          <SelectTrigger className='w-full'><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value='none'>{t('settings.apiCredential')}</SelectItem>
            {credentials.map((credential) => <SelectItem key={credential.id} value={String(credential.id)}>{credential.name}</SelectItem>)}
          </SelectContent>
        </Select>
      </Field>
      <Field>
        <FieldLabel>{t('settings.cloneHttps')}</FieldLabel>
        <LabeledSegmentedControl
          ariaLabel={t('settings.cloneHttps')}
          label={t('settings.cloneHttps')}
          onChange={(clone_protocol) => onChange({ ...form, clone_protocol })}
          options={[
            { value: 'https', label: t('settings.cloneHttps') },
            { value: 'ssh', label: t('settings.cloneSsh') }
          ]}
          value={form.clone_protocol}
        />
      </Field>
      {form.clone_protocol === 'ssh' ? (
        <>
          <Field>
            <FieldLabel htmlFor='scm-ssh-host'>{t('settings.sshHost')}</FieldLabel>
            <Input
              id='scm-ssh-host'
              value={form.ssh_host}
              onChange={(event) => onChange({ ...form, ssh_host: event.target.value })}
            />
          </Field>
          <Field>
            <FieldLabel>{t('settings.cloneCredential')}</FieldLabel>
            <Select value={form.clone_credential_id || 'none'} onValueChange={(value) => onChange({ ...form, clone_credential_id: value === 'none' ? '' : value })}>
              <SelectTrigger className='w-full'><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value='none'>{t('settings.cloneCredential')}</SelectItem>
                {credentials.map((credential) => <SelectItem key={credential.id} value={String(credential.id)}>{credential.name}</SelectItem>)}
              </SelectContent>
            </Select>
          </Field>
        </>
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
