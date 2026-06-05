import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { api } from '@/lib/api'

export function UserPage() {
  const qc = useQueryClient()
  const providers = useQuery({ queryKey: ['user-providers'], queryFn: api.userProviders })
  const createCredential = useMutation({
    mutationFn: ({ providerId, groupId }: { providerId: number; groupId: string }) => api.createGroupCredential(providerId, groupId),
    onSuccess: (result) => {
      void qc.invalidateQueries({ queryKey: ['user-providers'] })
      void navigator.clipboard?.writeText(result.secret)
      toast.success('Credential created and copied')
    }
  })
  const regenerateCredential = useMutation({
    mutationFn: ({ providerId, groupId }: { providerId: number; groupId: string }) => api.regenerateGroupCredential(providerId, groupId),
    onSuccess: (result) => {
      void qc.invalidateQueries({ queryKey: ['user-providers'] })
      void navigator.clipboard?.writeText(result.secret)
      toast.success('Credential regenerated and copied')
    }
  })

  if (providers.isLoading) return <LoadingState />

  return (
    <Page>
      <PageHeader title='My Setup' description='CLI setup and provider/group credential self-serve. Credential writes use the backend relay identity contract.' />
      <div className='grid gap-4 lg:grid-cols-[0.8fr_1.2fr]'>
        <Card>
          <CardHeader>
            <CardTitle>CLI workflow</CardTitle>
            <CardDescription>Commands are guidance only; the browser does not claim local CLI state.</CardDescription>
          </CardHeader>
          <CardContent className='flex flex-col gap-3'>
            <CommandBlock command='ae-cli login' />
            <CommandBlock command='ae-cli discover' />
            <CommandBlock command='ae-cli hooks enable --global' />
            <CommandBlock command='ae-cli init' />
            <CommandBlock command='ae-cli doctor' />
          </CardContent>
        </Card>
        <div className='flex flex-col gap-4'>
          {(providers.data?.providers ?? []).map((provider) => (
            <Card key={provider.id}>
              <CardHeader>
                <CardTitle>{provider.display_name || provider.name}</CardTitle>
                <CardDescription>{provider.base_url}</CardDescription>
              </CardHeader>
              <CardContent className='flex flex-col gap-3'>
                {provider.groups.map((group) => (
                  <div key={group.group_id} className='flex flex-col gap-3 rounded-md bg-muted p-3 md:flex-row md:items-center md:justify-between'>
                    <div>
                      <div className='font-medium'>{group.group_name}</div>
                      <div className='text-muted-foreground text-xs'>{group.platform} · {group.group_id}</div>
                    </div>
                    <div className='flex flex-wrap items-center gap-2'>
                      <Badge variant={group.credential.state === 'existing_hidden' ? 'success' : 'warning'}>{group.credential.state}</Badge>
                      {group.credential.state === 'missing' ? (
                        <Button size='sm' onClick={() => createCredential.mutate({ providerId: provider.id, groupId: group.group_id })}>
                          Create key
                        </Button>
                      ) : (
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() => {
                            if (window.confirm('Regenerate this credential? Existing local tool configs may need updating.')) {
                              regenerateCredential.mutate({ providerId: provider.id, groupId: group.group_id })
                            }
                          }}
                        >
                          Regenerate
                        </Button>
                      )}
                    </div>
                  </div>
                ))}
              </CardContent>
            </Card>
          ))}
        </div>
      </div>
    </Page>
  )
}

function CommandBlock({ command }: { command: string }) {
  return (
    <button
      className='flex items-center justify-between rounded-md border border-border bg-background px-3 py-2 text-left font-mono text-[var(--ae-ai-2)] text-xs'
      onClick={() => {
        void navigator.clipboard?.writeText(command)
        toast.success('Command copied')
      }}
    >
      <span>$ {command}</span>
      <span className='text-muted-foreground'>copy</span>
    </button>
  )
}
