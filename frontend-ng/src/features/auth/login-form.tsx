import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { AppAlert } from '@/components/primitives/app-alert'
import { SelectField } from '@/components/primitives/select-field'
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
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor='login-username'>{t('auth.usernameOrEmail')}</FieldLabel>
          <Input
            id='login-username'
            value={username}
            onChange={(event) => onUsernameChange(event.target.value)}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor='login-password'>{t('auth.password')}</FieldLabel>
          <Input
            id='login-password'
            type='password'
            value={password}
            onChange={(event) => onPasswordChange(event.target.value)}
          />
        </Field>
        <SelectField
          id='login-source'
          label={t('auth.loginSource')}
          options={[
            ...(options?.ldap_enabled ? [{ label: 'LDAP', value: 'LDAP' }] : []),
            { label: t('auth.relaySso'), value: 'SSO' }
          ]}
          triggerClassName='w-full'
          value={source}
          onValueChange={onSourceChange}
        />
        {error ? <AppAlert tone='error' title={error} /> : null}
        <Button disabled={!username || !password || pending}>
          {pending ? t('auth.signingIn') : t('auth.signIn')}
        </Button>
      </FieldGroup>
    </form>
  )
}
