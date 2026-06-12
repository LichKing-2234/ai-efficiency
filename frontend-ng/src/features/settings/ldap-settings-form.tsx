import { FieldGroup } from '@/components/ui/field'
import { AppAlert } from '@/components/primitives/app-alert'
import { CheckboxField } from '@/components/primitives/checkbox-field'
import { PrimaryActionButton } from '@/components/primitives/primary-action-button'
import { SecondaryActionButton } from '@/components/primitives/secondary-action-button'
import { StartActionsFeedback } from '@/components/primitives/start-actions-feedback'
import { TextField } from '@/components/primitives/text-field'
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
        <TextField
          id={field.id}
          key={field.key}
          label={t(field.labelKey)}
          type={field.type}
          value={form[field.key]}
          onChange={(value) => onChange({ ...form, [field.key]: value })}
        />
      ))}
      <CheckboxField id='ldap-starttls' checked={form.tls} label={t('settings.useStartTls')} onCheckedChange={(tls) => onChange({ ...form, tls })} />
      <StartActionsFeedback
        actions={(
          <>
            <SecondaryActionButton
              onClick={onTest}
              disabled={!requiredReady || testPending}
            >
              {t('settings.testLdap')}
            </SecondaryActionButton>
            <PrimaryActionButton
              onClick={onSave}
              disabled={!requiredReady || savePending}
            >
              {t('settings.saveLdap')}
            </PrimaryActionButton>
          </>
        )}
      >
        {message ? (
          <AppAlert
            tone={message.toLowerCase().includes('failed') || message.toLowerCase().includes('required') ? 'error' : 'success'}
            title={message}
          />
        ) : null}
      </StartActionsFeedback>
    </FieldGroup>
  )
}
