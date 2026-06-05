import { useMutation, useQuery } from '@tanstack/react-query'
import { useLocation, useNavigate, useSearch } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { apiFetch } from '@/lib/api/client'
import { ensureAuthenticatedUser } from '@/lib/auth/session'
import { buildLoginRedirect, buildOAuthAuthorizePayload, normalizeDeviceCode } from '@/features/auth/auth-flow-state'

export function OAuthAuthorizePage() {
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
        setError('Authorization response did not include a redirect URI.')
      }
    },
    onError: (mutationError) => {
      setError(mutationError instanceof Error ? mutationError.message : 'Authorization failed.')
    }
  })

  useEffect(() => {
    if (me.error) {
      const redirect = buildLoginRedirect(location.href)
      void navigate({ to: redirect.to, search: redirect.search as never })
    }
  }, [location.href, me.error, navigate])

  return (
    <AuthSurface title='Authorize ae-cli' description='Allow the CLI to receive an AI Efficiency app token for this account.'>
      <div className='rounded-md bg-muted p-3 text-sm'>
        Signed in as <span className='font-medium'>{me.data?.email || me.data?.username || 'current user'}</span>
      </div>
      {error ? <div className='text-[var(--ae-warn)] text-sm'>{error}</div> : null}
      <div className='flex gap-2'>
        <Button className='flex-1' disabled={approve.isPending || me.isLoading || !!me.error} onClick={() => approve.mutate(true)}>Approve</Button>
        <Button className='flex-1' disabled={approve.isPending || me.isLoading || !!me.error} variant='outline' onClick={() => approve.mutate(false)}>Deny</Button>
      </div>
    </AuthSurface>
  )
}

export function OAuthDevicePage() {
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
    <AuthSurface title='Device login' description='Enter the code shown by ae-cli on the other device.'>
      <div className='rounded-md bg-muted p-3 text-sm'>
        Signed in as <span className='font-medium'>{me.data?.email || me.data?.username || 'current user'}</span>
      </div>
      <Input value={code} onChange={(event) => setCode(normalizeDeviceCode(event.target.value))} placeholder='ABCD-EFGH' />
      {verify.data ? <div className='text-[var(--ae-pos)] text-sm'>Device {verify.data.status}.</div> : null}
      {verify.error ? <div className='text-[var(--ae-warn)] text-sm'>{verify.error.message}</div> : null}
      <div className='flex gap-2'>
        <Button className='flex-1' disabled={!code || verify.isPending || me.isLoading || !!me.error} onClick={() => verify.mutate(true)}>Approve</Button>
        <Button className='flex-1' disabled={!code || verify.isPending || me.isLoading || !!me.error} variant='outline' onClick={() => verify.mutate(false)}>Deny</Button>
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
