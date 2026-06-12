import { FieldGroup } from '@/components/ui/field'
import { AppAlert } from '@/components/primitives/app-alert'
import { authFieldControlClassName } from '@/components/primitives/auth-field'
import { AuthSubmitButton } from '@/components/primitives/auth-submit-button'
import { SelectField } from '@/components/primitives/select-field'
import { TextField } from '@/components/primitives/text-field'
import { useI18n } from '@/lib/i18n/i18n'
import type { AuthOptions } from '@/lib/api/types'

export function LoginForm({
  error,
  onPasswordChange,
  onSourceChange,
  onSubmit,
  onUsernameChange,
  options,
  password,
  pending,
  source,
  username
}: {
  error?: string
  onPasswordChange: (value: string) => void
  onSourceChange: (value: string) => void
  onSubmit: () => void
  onUsernameChange: (value: string) => void
  options?: AuthOptions
  password: string
  pending?: boolean
  source: string
  username: string
}) {
  const { t } = useI18n()

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault()
        onSubmit()
      }}
    >
      <FieldGroup gap='compact'>
        <TextField
          id='login-username'
          label={t('auth.usernameOrEmail')}
          controlClassName={authFieldControlClassName}
          value={username}
          onChange={onUsernameChange}
        />
        <TextField
          id='login-password'
          label={t('auth.password')}
          controlClassName={authFieldControlClassName}
          type='password'
          value={password}
          onChange={onPasswordChange}
        />
        <SelectField
          id='login-source'
          label={t('auth.loginSource')}
          options={[
            ...(options?.ldap_enabled ? [{ label: 'LDAP', value: 'LDAP' }] : []),
            { label: t('auth.relaySso'), value: 'SSO' }
          ]}
          triggerClassName={`${authFieldControlClassName} w-full`}
          value={source}
          onValueChange={onSourceChange}
        />
        {error ? <AppAlert tone='error' title={error} description={t('auth.loginErrorDescription')} /> : null}
        <AuthSubmitButton disabled={!username || !password || pending}>
          {pending ? t('auth.signingIn') : t('auth.signIn')}
        </AuthSubmitButton>
      </FieldGroup>
    </form>
  )
}
