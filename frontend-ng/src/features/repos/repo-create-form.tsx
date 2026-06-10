import { Button } from '@/components/ui/button'
import { FieldGroup } from '@/components/ui/field'
import { ActionGroup } from '@/components/primitives/action-group'
import { AppAlert } from '@/components/primitives/app-alert'
import { FieldItem, FieldList } from '@/components/primitives/field-list'
import { InsetPanel } from '@/components/primitives/inset-panel'
import { LabeledSegmentedControl } from '@/components/primitives/labeled-segmented-control'
import { SelectField } from '@/components/primitives/select-field'
import { TextField } from '@/components/primitives/text-field'
import type { SCMProvider } from '@/lib/api/types'
import type { ParsedRepoUrl, RepoCloneProtocol } from './repos-state'

export interface RepoCreateFormLabels {
  cancel: string
  clone: string
  create: string
  defaultBranch: string
  enterRepoUrl: string
  fullName: string
  noMatchingProvider: string
  provider: string
  previewCloneUrl: string
  repoUrl: string
  repoUrlPlaceholder: string
  selectScmProvider: string
  sshHostExample: string
}

export function RepoCreateForm({
  addError,
  cloneProtocol,
  createPending,
  defaultBranch,
  labels,
  onCancel,
  onCloneProtocolChange,
  onCreate,
  onDefaultBranchChange,
  onRepoUrlChange,
  onSelectedProviderIdChange,
  onSshHostChange,
  parsedRepo,
  previewCloneUrl,
  providers,
  repoUrl,
  selectedProvider,
  selectedProviderId,
  sshHost
}: {
  addError: string
  cloneProtocol: RepoCloneProtocol
  createPending: boolean
  defaultBranch: string
  labels: RepoCreateFormLabels
  onCancel: () => void
  onCloneProtocolChange: (protocol: RepoCloneProtocol) => void
  onCreate: () => void
  onDefaultBranchChange: (branch: string) => void
  onRepoUrlChange: (url: string) => void
  onSelectedProviderIdChange: (id: string) => void
  onSshHostChange: (host: string) => void
  parsedRepo: ParsedRepoUrl | null
  previewCloneUrl: string
  providers: SCMProvider[]
  repoUrl: string
  selectedProvider?: SCMProvider
  selectedProviderId: string
  sshHost: string
}) {
  return (
    <FieldGroup className='gap-3'>
      <SelectField
        id='repo-create-provider'
        label={labels.provider}
        options={providers.map((provider) => ({ label: provider.name, value: String(provider.id) }))}
        placeholder={labels.selectScmProvider}
        triggerClassName='w-full'
        value={selectedProviderId}
        onValueChange={onSelectedProviderIdChange}
      />
      <TextField
        id='repo-create-url'
        label={labels.repoUrl}
        placeholder={labels.repoUrlPlaceholder}
        value={repoUrl}
        onChange={onRepoUrlChange}
      />
      {parsedRepo ? (
        <InsetPanel stack>
          <FieldList>
            <FieldItem label={labels.fullName} value={`${parsedRepo.project}/${parsedRepo.repo}`} truncate />
            <FieldItem label={labels.provider} value={selectedProvider?.name || labels.noMatchingProvider} truncate />
          </FieldList>
          <LabeledSegmentedControl
            ariaLabel={labels.clone}
            label={labels.clone}
            onChange={onCloneProtocolChange}
            options={[
              { value: 'http', label: 'HTTP' },
              { value: 'ssh', label: 'SSH' }
            ]}
            value={cloneProtocol}
          />
          {cloneProtocol === 'ssh' && parsedRepo.type === 'bitbucket' ? (
            <TextField
              id='repo-create-ssh-host'
              label={labels.sshHostExample}
              placeholder={labels.sshHostExample}
              value={sshHost}
              onChange={onSshHostChange}
            />
          ) : null}
          <TextField
            controlClassName='font-mono text-xs'
            id='repo-create-preview-clone-url'
            label={labels.previewCloneUrl}
            readOnly
            value={previewCloneUrl}
          />
        </InsetPanel>
      ) : repoUrl ? (
        <AppAlert tone='warning' title={labels.enterRepoUrl} />
      ) : null}
      <TextField id='repo-create-default-branch' label={labels.defaultBranch} value={defaultBranch} onChange={onDefaultBranchChange} />
      {addError ? <AppAlert tone='error' title={addError} /> : null}
      <ActionGroup>
        <Button variant='outline' onClick={onCancel}>{labels.cancel}</Button>
        <Button disabled={!selectedProviderId || !parsedRepo || createPending} onClick={onCreate}>{labels.create}</Button>
      </ActionGroup>
    </FieldGroup>
  )
}
