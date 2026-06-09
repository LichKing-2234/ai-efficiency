import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ActionGroup } from '@/components/primitives/action-group'
import { AppAlert } from '@/components/primitives/app-alert'
import { useI18n } from '@/lib/i18n/i18n'
import type { LDAPFormState } from './settings-payloads'

type LDAPField = keyof Pick<LDAPFormState, 'url' | 'base_dn' | 'bind_dn' | 'bind_password' | 'user_filter'>

const ldapFields: Array<{
  id: string
  key: LDAPField
  labelKey: Parameters<ReturnType<typeof useI18n>['t']>[0]
  type?: React.HTMLInputTypeAttribute
}> = [
  { id: 'ldap-url', key: 'url', labelKey: 'settings.ldapUrl' },
  { id: 'ldap-base-dn', key: 'base_dn', labelKey: 'settings.baseDn' },
  { id: 'ldap-bind-dn', key: 'bind_dn', labelKey: 'settings.bindDn' },
  { id: 'ldap-bind-password', key: 'bind_password', labelKey: 'settings.bindPassword', type: 'password' },
  { id: 'ldap-user-filter', key: 'user_filter', labelKey: 'settings.userFilter' }
]

export function LdapSettingsForm({
  form,
  message,
  onChange,
  onSave,
  onTest,
  savePending,
  testPending
}: {
  form: LDAPFormState
  message?: string
  onChange: (form: LDAPFormState) => void
  onSave: () => void
  onTest: () => void
  savePending?: boolean
  testPending?: boolean
}) {
  const { t } = useI18n()
  const requiredReady = !!form.url && !!form.base_dn && !!form.bind_dn && !!form.user_filter

  return (
    <FieldGroup>
      {ldapFields.map((field) => (
        <Field key={field.key}>
          <FieldLabel htmlFor={field.id}>{t(field.labelKey)}</FieldLabel>
          <Input
            id={field.id}
            type={field.type}
            value={form[field.key]}
            onChange={(event) => onChange({ ...form, [field.key]: event.target.value })}
          />
        </Field>
      ))}
      <Field orientation='horizontal'>
        <Checkbox
          id='ldap-starttls'
          checked={form.tls}
          onCheckedChange={(checked) => onChange({ ...form, tls: checked === true })}
        />
        <FieldLabel htmlFor='ldap-starttls'>{t('settings.useStartTls')}</FieldLabel>
      </Field>
      {message ? (
        <AppAlert
          tone={message.toLowerCase().includes('failed') || message.toLowerCase().includes('required') ? 'error' : 'success'}
          title={message}
        />
      ) : null}
      <ActionGroup wrap className='justify-start'>
        <Button
          variant='outline'
          onClick={onTest}
          disabled={!requiredReady || testPending}
        >
          {t('settings.testLdap')}
        </Button>
        <Button
          onClick={onSave}
          disabled={!requiredReady || savePending}
        >
          {t('settings.saveLdap')}
        </Button>
      </ActionGroup>
    </FieldGroup>
  )
}
