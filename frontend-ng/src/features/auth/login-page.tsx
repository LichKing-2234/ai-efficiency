import { useNavigate, useSearch } from '@tanstack/react-router'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'

export function LoginPage() {
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as { redirect?: string }
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [source, setSource] = useState('ldap')
  const options = useQuery({ queryKey: ['auth', 'options'], queryFn: api.auth.options })
  const login = useMutation({
    mutationFn: () => api.auth.login({ username, password, source }),
    onSuccess: () => navigate({ to: safeRedirect(search.redirect) })
  })
  const devLogin = useMutation({
    mutationFn: api.auth.devLogin,
    onSuccess: () => navigate({ to: safeRedirect(search.redirect) })
  })

  return (
    <main className='grid min-h-screen place-items-center bg-background p-4'>
      <Card className='w-full max-w-md'>
        <CardHeader>
          <CardTitle>Sign in to AI Efficiency</CardTitle>
          <CardDescription>Manual login is a fallback. Gateway-authenticated deployments bootstrap automatically.</CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className='flex flex-col gap-3'
            onSubmit={(event) => {
              event.preventDefault()
              login.mutate()
            }}
          >
            <Input placeholder='Username or email' value={username} onChange={(event) => setUsername(event.target.value)} />
            <Input placeholder='Password' type='password' value={password} onChange={(event) => setPassword(event.target.value)} />
            <select
              className='h-8 rounded-md border border-input bg-card px-3 text-sm'
              value={source}
              onChange={(event) => setSource(event.target.value)}
            >
              {options.data?.ldap_enabled ? <option value='ldap'>LDAP</option> : null}
              <option value='sso'>Relay SSO</option>
            </select>
            {login.error ? <div className='text-[var(--ae-warn)] text-sm'>{login.error.message}</div> : null}
            <Button disabled={!username || !password || login.isPending}>
              {login.isPending ? 'Signing in...' : 'Sign in'}
            </Button>
          </form>
          {options.data?.dev_login_enabled ? (
            <Button className='mt-3 w-full' variant='outline' onClick={() => devLogin.mutate()} disabled={devLogin.isPending}>
              Dev Login
            </Button>
          ) : null}
        </CardContent>
      </Card>
    </main>
  )
}

function safeRedirect(raw?: string) {
  if (!raw || !raw.startsWith('/') || raw.startsWith('//') || raw.startsWith('/login')) return '/'
  return raw
}
