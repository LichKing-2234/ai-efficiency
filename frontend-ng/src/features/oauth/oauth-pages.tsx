import { useMutation, useQuery } from '@tanstack/react-query'
import { useLocation, useNavigate, useSearch } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { ActionGroup } from '@/components/primitives/action-group'
import { AppAlert } from '@/components/primitives/app-alert'
import { AuthInfoPanel } from '@/components/primitives/auth-info-panel'
import { AuthSurface } from '@/components/primitives/auth-surface'
import { TextField } from '@/components/primitives/text-field'
import { apiFetch } from '@/lib/api/client'
import { ensureAuthenticatedUser } from '@/lib/auth/session'
import { useI18n } from '@/lib/i18n/i18n'
import {
  buildLoginRedirect,
  buildCurrentRouteRedirectPath,
  buildOAuthAuthorizePayload,
  buildPublicOAuthAuthQueryOptions,
  normalizeDeviceCode,
  shouldNavigateToLoginRedirect
} from '@/features/auth/auth-flow-state'

export function OAuthAuthorizePage() {
  const { t } = useI18n()
  const search = useSearch({ strict: false }) as Record<string, string>
  const location = useLocation()
  const navigate = useNavigate()
  const [error, setError] = useState('')
  const me = useQuery({ ...buildPublicOAuthAuthQueryOptions('oauth'), queryFn: ensureAuthenticatedUser })
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
      const redirect = buildLoginRedirect(buildCurrentRouteRedirectPath(location.pathname, location.searchStr))
      if (shouldNavigateToLoginRedirect({ currentPath: location.pathname, redirect })) {
        void navigate({ to: redirect.to, search: redirect.search as never, replace: true })
      }
    }
  }, [location.pathname, location.searchStr, me.error, navigate])

  return (
    <AuthSurface title={t('oauth.authorizeCli')} description={t('oauth.allowCli')}>
      <AuthInfoPanel>
        {t('oauth.signedInAs', { identity: me.data?.email || me.data?.username || t('auth.guest') })}
      </AuthInfoPanel>
      {error ? <AppAlert tone='error' title={error} /> : null}
      <OAuthActionGroup
        approveLabel={t('oauth.approve')}
        denyLabel={t('oauth.denied')}
        disabled={approve.isPending || me.isLoading || !!me.error}
        onApprove={() => approve.mutate(true)}
        onDeny={() => approve.mutate(false)}
      />
    </AuthSurface>
  )
}

export function OAuthDevicePage() {
  const { t } = useI18n()
  const location = useLocation()
  const navigate = useNavigate()
  const [code, setCode] = useState('')
  const me = useQuery({ ...buildPublicOAuthAuthQueryOptions('oauth-device'), queryFn: ensureAuthenticatedUser })
  const verify = useMutation({
    mutationFn: (approved: boolean) =>
      apiFetch<{ status: 'approved' | 'denied' }>('/oauth/device/verify', {
        method: 'POST',
        body: JSON.stringify({ user_code: normalizeDeviceCode(code), approved })
      })
  })

  useEffect(() => {
    if (me.error) {
      const redirect = buildLoginRedirect(buildCurrentRouteRedirectPath(location.pathname, location.searchStr))
      if (shouldNavigateToLoginRedirect({ currentPath: location.pathname, redirect })) {
        void navigate({ to: redirect.to, search: redirect.search as never, replace: true })
      }
    }
  }, [location.pathname, location.searchStr, me.error, navigate])

  return (
    <AuthSurface title={t('oauth.deviceLogin')} description={t('oauth.enterCode')}>
      <AuthInfoPanel>
        {t('oauth.signedInAs', { identity: me.data?.email || me.data?.username || t('auth.guest') })}
      </AuthInfoPanel>
      <DeviceCodeField
        code={code}
        label={t('oauth.deviceCode')}
        placeholder='ABCD-EFGH'
        onCodeChange={(value) => setCode(normalizeDeviceCode(value))}
      />
      {verify.data ? <AppAlert tone='success' title={t('oauth.deviceStatus', { status: verify.data.status })} /> : null}
      {verify.error ? <AppAlert tone='error' title={verify.error.message} /> : null}
      <OAuthActionGroup
        approveLabel={t('oauth.approve')}
        denyLabel={t('oauth.denied')}
        disabled={!code || verify.isPending || me.isLoading || !!me.error}
        onApprove={() => verify.mutate(true)}
        onDeny={() => verify.mutate(false)}
      />
    </AuthSurface>
  )
}

export function DeviceCodeField({
  code,
  label,
  onCodeChange,
  placeholder
}: {
  code: string
  label: string
  onCodeChange: (value: string) => void
  placeholder: string
}) {
  return (
    <TextField
      id='oauth-device-code'
      label={label}
      placeholder={placeholder}
      value={code}
      onChange={onCodeChange}
    />
  )
}

export function OAuthActionGroup({
  approveLabel,
  denyLabel,
  disabled,
  onApprove,
  onDeny
}: {
  approveLabel: string
  denyLabel: string
  disabled: boolean
  onApprove: () => void
  onDeny: () => void
}) {
  return (
    <ActionGroup layout='split'>
      <Button disabled={disabled} onClick={onApprove}>{approveLabel}</Button>
      <Button disabled={disabled} variant='outline' onClick={onDeny}>{denyLabel}</Button>
    </ActionGroup>
  )
}
