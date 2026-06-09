import { useMutation, useQuery } from '@tanstack/react-query'
import { useLocation, useNavigate, useSearch } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { AppAlert } from '@/components/primitives/app-alert'
import { AuthInfoPanel } from '@/components/primitives/auth-info-panel'
import { apiFetch } from '@/lib/api/client'
import { ensureAuthenticatedUser } from '@/lib/auth/session'
import { useI18n } from '@/lib/i18n/i18n'
import { buildLoginRedirect, buildOAuthAuthorizePayload, normalizeDeviceCode } from '@/features/auth/auth-flow-state'

export function OAuthAuthorizePage() {
  const { t } = useI18n()
  const search = useSearch({ strict: false }) as Record<string, string>
  const location = useLocation()
  const navigate = useNavigate()
  const [error, setError] = useState('')
  const me = useQuery({ queryKey: ['auth', 'me', 'oauth'], queryFn: ensureAuthenticatedUser })
  const approve = useMutation({
    mutationFn: (approved: boolean) =>
      apiFetch<{ redirect_uri: string }>('/oauth/authorize/approve', {
        method: 'POST',
        body: JSON.stringify(buildOAuthAuthorizePayload(search, approved))
      }),
    onSuccess: (data) => {
      if (data.redirect_uri) {
        window.location.href = data.redirect_uri
      } else {
        setError(t('oauth.redirectMissing'))
      }
    },
    onError: (mutationError) => {
      setError(mutationError instanceof Error ? mutationError.message : t('oauth.authorizationFailed'))
    }
  })

  useEffect(() => {
    if (me.error) {
      const redirect = buildLoginRedirect(location.href)
      void navigate({ to: redirect.to, search: redirect.search as never })
    }
  }, [location.href, me.error, navigate])

  return (
    <AuthSurface title={t('oauth.authorizeCli')} description={t('oauth.allowCli')}>
      <AuthInfoPanel>
        {t('oauth.signedInAs', { identity: me.data?.email || me.data?.username || t('auth.guest') })}
      </AuthInfoPanel>
      {error ? <AppAlert tone='error' title={error} /> : null}
      <div className='flex gap-2'>
        <Button className='flex-1' disabled={approve.isPending || me.isLoading || !!me.error} onClick={() => approve.mutate(true)}>{t('oauth.approve')}</Button>
        <Button className='flex-1' disabled={approve.isPending || me.isLoading || !!me.error} variant='outline' onClick={() => approve.mutate(false)}>{t('oauth.denied')}</Button>
      </div>
    </AuthSurface>
  )
}

export function OAuthDevicePage() {
  const { t } = useI18n()
  const location = useLocation()
  const navigate = useNavigate()
  const [code, setCode] = useState('')
  const me = useQuery({ queryKey: ['auth', 'me', 'oauth-device'], queryFn: ensureAuthenticatedUser })
  const verify = useMutation({
    mutationFn: (approved: boolean) =>
      apiFetch<{ status: 'approved' | 'denied' }>('/oauth/device/verify', {
        method: 'POST',
        body: JSON.stringify({ user_code: normalizeDeviceCode(code), approved })
      })
  })

  useEffect(() => {
    if (me.error) {
      const redirect = buildLoginRedirect(location.href)
      void navigate({ to: redirect.to, search: redirect.search as never })
    }
  }, [location.href, me.error, navigate])

  return (
    <AuthSurface title={t('oauth.deviceLogin')} description={t('oauth.enterCode')}>
      <AuthInfoPanel>
        {t('oauth.signedInAs', { identity: me.data?.email || me.data?.username || t('auth.guest') })}
      </AuthInfoPanel>
      <Input value={code} onChange={(event) => setCode(normalizeDeviceCode(event.target.value))} placeholder='ABCD-EFGH' />
      {verify.data ? <AppAlert tone='success' title={t('oauth.deviceStatus', { status: verify.data.status })} /> : null}
      {verify.error ? <AppAlert tone='error' title={verify.error.message} /> : null}
      <div className='flex gap-2'>
        <Button className='flex-1' disabled={!code || verify.isPending || me.isLoading || !!me.error} onClick={() => verify.mutate(true)}>{t('oauth.approve')}</Button>
        <Button className='flex-1' disabled={!code || verify.isPending || me.isLoading || !!me.error} variant='outline' onClick={() => verify.mutate(false)}>{t('oauth.denied')}</Button>
      </div>
    </AuthSurface>
  )
}

function AuthSurface({ title, description, children }: { title: string; description: string; children: React.ReactNode }) {
  return (
    <main className='grid min-h-screen place-items-center bg-background p-4'>
      <Card className='w-full max-w-md'>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
          <CardDescription>{description}</CardDescription>
        </CardHeader>
        <CardContent className='flex flex-col gap-3'>{children}</CardContent>
      </Card>
    </main>
  )
}
