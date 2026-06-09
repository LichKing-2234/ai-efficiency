import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { AppAlert } from '@/components/primitives/app-alert'
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
        <Field>
          <FieldLabel>{t('auth.loginSource')}</FieldLabel>
          <Select value={source} onValueChange={onSourceChange}>
            <SelectTrigger className='w-full'><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {options?.ldap_enabled ? <SelectItem value='LDAP'>LDAP</SelectItem> : null}
                <SelectItem value='SSO'>{t('auth.relaySso')}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
        {error ? <AppAlert tone='error' title={error} /> : null}
        <Button disabled={!username || !password || pending}>
          {pending ? t('auth.signingIn') : t('auth.signIn')}
        </Button>
      </FieldGroup>
    </form>
  )
}
