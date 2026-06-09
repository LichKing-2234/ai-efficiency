import { useNavigate, useSearch } from '@tanstack/react-router'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { AppAlert } from '@/components/primitives/app-alert'
import { SectionCardHeader } from '@/components/primitives/section-card-header'
import { api } from '@/lib/api'
import { useI18n } from '@/lib/i18n/i18n'
import { safeRedirect, selectInitialLoginSource } from './auth-flow-state'

export function LoginPage() {
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
    <main className='grid min-h-screen place-items-center bg-background p-4'>
      <Card className='w-full max-w-md'>
        <SectionCardHeader title={t('auth.loginTitle')} description={t('auth.loginDescription')} />
        <CardContent>
          <form
            className='flex flex-col gap-3'
            onSubmit={(event) => {
              event.preventDefault()
              login.mutate()
            }}
          >
            <Input placeholder={t('auth.usernameOrEmail')} value={username} onChange={(event) => setUsername(event.target.value)} />
            <Input placeholder={t('auth.password')} type='password' value={password} onChange={(event) => setPassword(event.target.value)} />
            <Select value={source} onValueChange={setSource}>
              <SelectTrigger className='w-full'><SelectValue /></SelectTrigger>
              <SelectContent>
                {options.data?.ldap_enabled ? <SelectItem value='LDAP'>LDAP</SelectItem> : null}
                <SelectItem value='SSO'>{t('auth.relaySso')}</SelectItem>
              </SelectContent>
            </Select>
            {login.error ? <AppAlert tone='error' title={login.error.message} /> : null}
            <Button disabled={!username || !password || login.isPending}>
              {login.isPending ? t('auth.signingIn') : t('auth.signIn')}
            </Button>
          </form>
          {options.data?.dev_login_enabled ? (
            <Button className='mt-3 w-full' variant='outline' onClick={() => devLogin.mutate()} disabled={devLogin.isPending}>
              {t('auth.devLogin')}
            </Button>
          ) : null}
        </CardContent>
      </Card>
    </main>
  )
}
