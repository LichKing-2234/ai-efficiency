import { useNavigate, useSearch } from '@tanstack/react-router'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { AuthInfoPanel } from '@/components/primitives/auth-info-panel'
import { LinkAction } from '@/components/primitives/link-action'
import { QuietActionButton } from '@/components/primitives/quiet-action-button'
import { AuthSurface } from '@/components/primitives/auth-surface'
import { StartActions } from '@/components/primitives/start-actions'
import { api } from '@/lib/api'
import type { AuthOptions } from '@/lib/api/types'
import { useI18n } from '@/lib/i18n/i18n'
import { safeRedirect, selectInitialLoginSource } from './auth-flow-state'
import { LoginForm } from './login-form'

export type LoginPageProps = {
  initialOptions?: AuthOptions | null
  localHandoffHref?: string | null
}

export function LoginPage({ initialOptions = null, localHandoffHref = null }: LoginPageProps) {
  const { t } = useI18n()
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as { redirect?: string }
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [source, setSource] = useState(selectInitialLoginSource(initialOptions))
  const [resolvedLocalHandoffHref, setResolvedLocalHandoffHref] = useState(localHandoffHref)
  const options = useQuery({ queryKey: ['auth', 'options'], queryFn: api.auth.options, initialData: initialOptions ?? undefined })
  const login = useMutation({
    mutationFn: () => api.auth.login({ username, password, source }),
    onSuccess: () => navigate({ to: safeRedirect(search.redirect) })
  })

  useEffect(() => {
    if (options.data) setSource(selectInitialLoginSource(options.data))
  }, [options.data])

  useEffect(() => {
    if (!localHandoffHref || typeof window === 'undefined') {
      setResolvedLocalHandoffHref(localHandoffHref)
      return
    }

    try {
      const href = new URL(localHandoffHref)
      href.searchParams.set('target', window.location.origin)
      setResolvedLocalHandoffHref(href.toString())
    } catch {
      setResolvedLocalHandoffHref(null)
    }
  }, [localHandoffHref])

  const devLogin = useMutation({
    mutationFn: api.auth.devLogin,
    onSuccess: () => navigate({ to: safeRedirect(search.redirect) })
  })

  return (
    <AuthSurface
      title={t('auth.loginTitle')}
      description={t('auth.loginDescription')}
      actions={(
        <StartActions>
          {options.data?.dev_login_enabled ? (
            <QuietActionButton disabled={devLogin.isPending} onClick={() => devLogin.mutate()}>
              {t('auth.devLogin')}
            </QuietActionButton>
          ) : null}
          {resolvedLocalHandoffHref ? (
            <LinkAction asChild variant='outline'>
              <a href={resolvedLocalHandoffHref}>{t('auth.localHandoff')}</a>
            </LinkAction>
          ) : null}
        </StartActions>
      )}
    >
      <AuthInfoPanel>{t('auth.localHandoffDescription')}</AuthInfoPanel>
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
