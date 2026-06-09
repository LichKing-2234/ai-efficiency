import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { RepoCreateForm } from './repo-create-form'
import type { ParsedRepoUrl } from './repos-state'
import type { SCMProvider } from '@/lib/api/types'

const providers: SCMProvider[] = [
  provider(1, 'GitHub', 'https://github.com'),
  provider(2, 'Bitbucket', 'https://bitbucket.example.com')
]

const parsedRepo: ParsedRepoUrl = {
  origin: 'https://bitbucket.example.com',
  project: 'PROJ',
  repo: 'service',
  type: 'bitbucket'
}

describe('RepoCreateForm', () => {
  test('renders create controls through shadcn field primitives', () => {
    const html = renderToStaticMarkup(
      <RepoCreateForm
        addError=''
        cloneProtocol='http'
        createPending={false}
        defaultBranch='main'
        labels={labels()}
        parsedRepo={parsedRepo}
        previewCloneUrl='https://bitbucket.example.com/scm/proj/service.git'
        providers={providers}
        repoUrl='https://bitbucket.example.com/projects/PROJ/repos/service/browse'
        selectedProvider={providers[1]}
        selectedProviderId='2'
        sshHost=''
        onCancel={() => undefined}
        onCloneProtocolChange={() => undefined}
        onCreate={() => undefined}
        onDefaultBranchChange={() => undefined}
        onRepoUrlChange={() => undefined}
        onSelectedProviderIdChange={() => undefined}
        onSshHostChange={() => undefined}
      />
    )

    expect(html).toContain('data-slot="field-group"')
    expect(html).toContain('for="repo-create-provider"')
    expect(html).toContain('for="repo-create-url"')
    expect(html).toContain('for="repo-create-preview-clone-url"')
    expect(html).toContain('for="repo-create-default-branch"')
    expect(html).toContain('data-slot="action-group"')
    expect(html).toContain('PROJ/service')
  })

  test('renders SSH host field only for bitbucket SSH clone previews', () => {
    const html = renderToStaticMarkup(
      <RepoCreateForm
        addError=''
        cloneProtocol='ssh'
        createPending={false}
        defaultBranch='main'
        labels={labels()}
        parsedRepo={parsedRepo}
        previewCloneUrl='ssh://git@bitbucket.example.com/proj/service.git'
        providers={providers}
        repoUrl='https://bitbucket.example.com/projects/PROJ/repos/service/browse'
        selectedProvider={providers[1]}
        selectedProviderId='2'
        sshHost='bitbucket.example.com'
        onCancel={() => undefined}
        onCloneProtocolChange={() => undefined}
        onCreate={() => undefined}
        onDefaultBranchChange={() => undefined}
        onRepoUrlChange={() => undefined}
        onSelectedProviderIdChange={() => undefined}
        onSshHostChange={() => undefined}
      />
    )

    expect(html).toContain('for="repo-create-ssh-host"')
    expect(html).toContain('ssh://git@bitbucket.example.com/proj/service.git')
  })
})

function labels() {
  return {
    cancel: 'Cancel',
    clone: 'Clone',
    create: 'Create',
    defaultBranch: 'Default branch',
    enterRepoUrl: 'Enter a repository URL.',
    fullName: 'Full name',
    noMatchingProvider: 'No matching provider',
    previewCloneUrl: 'Preview clone URL',
    provider: 'Provider',
    repoUrl: 'Repository URL',
    repoUrlPlaceholder: 'https://github.com/org/repo',
    selectScmProvider: 'Select SCM provider',
    sshHostExample: 'SSH host, for example git.example.com'
  }
}

function provider(id: number, name: string, baseUrl: string): SCMProvider {
  return {
    id,
    name,
    type: 'github',
    base_url: baseUrl,
    status: 'active',
    created_at: '2026-01-01T00:00:00Z'
  }
}
