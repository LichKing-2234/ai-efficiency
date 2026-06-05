import { useMutation, useQuery } from '@tanstack/react-query'
import { useSearch } from '@tanstack/react-router'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { apiFetch } from '@/lib/api/client'
import { ensureAuthenticatedUser } from '@/lib/auth/session'

export function OAuthAuthorizePage() {
  const search = useSearch({ strict: false }) as Record<string, string>
  const me = useQuery({ queryKey: ['auth', 'me', 'oauth'], queryFn: ensureAuthenticatedUser })
  const approve = useMutation({
    mutationFn: (approved: boolean) =>
      apiFetch<{ redirect_uri: string }>('/oauth/authorize/approve', {
        method: 'POST',
        body: JSON.stringify({
          client_id: search.client_id,
          redirect_uri: search.redirect_uri,
          code_challenge: search.code_challenge,
          code_challenge_method: search.code_challenge_method,
          state: search.state,
          approved
        })
      }),
    onSuccess: (data) => {
      window.location.href = data.redirect_uri
    }
  })

  return (
    <AuthSurface title='Authorize ae-cli' description='Allow the CLI to receive an AI Efficiency app token for this account.'>
      <div className='rounded-md bg-muted p-3 text-sm'>
        Signed in as <span className='font-medium'>{me.data?.email || me.data?.username || 'current user'}</span>
      </div>
      {approve.error ? <div className='text-[var(--ae-warn)] text-sm'>{approve.error.message}</div> : null}
      <div className='flex gap-2'>
        <Button className='flex-1' onClick={() => approve.mutate(true)}>Approve</Button>
        <Button className='flex-1' variant='outline' onClick={() => approve.mutate(false)}>Deny</Button>
      </div>
    </AuthSurface>
  )
}

export function OAuthDevicePage() {
  const [code, setCode] = useState('')
  const verify = useMutation({
    mutationFn: (approved: boolean) =>
      apiFetch<{ status: 'approved' | 'denied' }>('/oauth/device/verify', {
        method: 'POST',
        body: JSON.stringify({ user_code: code, approved })
      })
  })
  return (
    <AuthSurface title='Device login' description='Enter the code shown by ae-cli on the other device.'>
      <Input value={code} onChange={(event) => setCode(event.target.value.toUpperCase())} placeholder='ABCD-EFGH' />
      {verify.data ? <div className='text-[var(--ae-pos)] text-sm'>Device {verify.data.status}.</div> : null}
      {verify.error ? <div className='text-[var(--ae-warn)] text-sm'>{verify.error.message}</div> : null}
      <div className='flex gap-2'>
        <Button className='flex-1' disabled={!code} onClick={() => verify.mutate(true)}>Approve</Button>
        <Button className='flex-1' disabled={!code} variant='outline' onClick={() => verify.mutate(false)}>Deny</Button>
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
