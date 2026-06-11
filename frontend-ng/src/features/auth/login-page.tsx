import { useNavigate, useSearch } from '@tanstack/react-router'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { AuthSurface } from '@/components/primitives/auth-surface'
import { InsetPanel } from '@/components/primitives/inset-panel'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import { useI18n } from '@/lib/i18n/i18n'
import { safeRedirect, selectInitialLoginSource } from './auth-flow-state'
import { LoginForm } from './login-form'

export type LoginPageProps = {
  localHandoffHref?: string | null
}

export function LoginPage({ localHandoffHref = null }: LoginPageProps) {
  const { t } = useI18n()
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as { redirect?: string }
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [source, setSource] = useState('SSO')
  const options = useQuery({ queryKey: ['auth', 'options'], queryFn: api.auth.options })
  const login = useMutation({
    mutationFn: () => api.auth.login({ username, password, source }),
    onSuccess: () => navigate({ to: safeRedirect(search.redirect) })
  })

  useEffect(() => {
    if (options.data) setSource(selectInitialLoginSource(options.data))
  }, [options.data])

  const devLogin = useMutation({
    mutationFn: api.auth.devLogin,
    onSuccess: () => navigate({ to: safeRedirect(search.redirect) })
  })

  return (
    <AuthSurface
      title={t('auth.loginTitle')}
      description={t('auth.loginDescription')}
      actions={(
        <div className='flex flex-col gap-2 sm:flex-row'>
          {options.data?.dev_login_enabled ? (
            <Button disabled={devLogin.isPending} onClick={() => devLogin.mutate()} variant='outline'>
              {t('auth.devLogin')}
            </Button>
          ) : null}
          {localHandoffHref ? (
            <Button asChild variant='outline'>
              <a href={localHandoffHref}>{t('auth.localHandoff')}</a>
            </Button>
          ) : null}
        </div>
      )}
    >
      <InsetPanel muted>
        <div className='text-[12px] text-[var(--ink-3)]'>{t('auth.localHandoffDescription')}</div>
      </InsetPanel>
      <LoginForm
        error={login.error?.message}
        options={options.data}
        password={password}
        pending={login.isPending}
        source={source}
        username={username}
        onPasswordChange={setPassword}
        onSourceChange={setSource}
        onSubmit={() => login.mutate()}
        onUsernameChange={setUsername}
      />
    </AuthSurface>
  )
}
