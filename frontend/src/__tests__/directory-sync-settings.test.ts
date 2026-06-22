import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import DirectorySyncSettings from '@/components/settings/DirectorySyncSettings.vue'

vi.mock('@/api/directory', () => ({
  listDirectorySources: vi.fn(),
  createDirectorySource: vi.fn(),
  updateDirectorySource: vi.fn(),
  validateDirectorySource: vi.fn(),
  previewDirectorySource: vi.fn(),
  startDirectoryRun: vi.fn(),
}))

Object.assign(navigator, {
  clipboard: {
    writeText: vi.fn(),
  },
})

async function mountDirectorySyncSettings() {
  const api = await import('@/api/directory') as any
  api.listDirectorySources.mockResolvedValue({
    data: {
      data: {
        items: [
          {
            id: 1,
            name: 'Example Directory',
            description: 'Synthetic directory source',
            scope: 'full_company',
            enabled: true,
            dsl: 'version: 1\nscope: full_company\n',
            schedule_enabled: false,
            schedule_interval: 'daily',
            schedule_timezone: 'UTC',
          },
        ],
      },
    },
  })
  api.updateDirectorySource.mockResolvedValue({ data: { data: { id: 1, name: 'Example Directory' } } })
  api.validateDirectorySource.mockResolvedValue({ data: { data: { valid: true, issues: [] } } })
  api.previewDirectorySource.mockResolvedValue({ data: { data: { id: 10, mode: 'preview', status: 'completed' } } })
  api.startDirectoryRun.mockResolvedValue({ data: { data: { id: 11, mode: 'apply', status: 'completed' } } })

  const wrapper = mount(DirectorySyncSettings, {
    props: {
      credentials: [{ id: 3, name: 'directory_api_key', kind: 'secret_text', description: '', usage_count: 0, summary: {}, created_at: '', updated_at: '' }],
    },
  })
  await flushPromises()
  return { wrapper, api }
}

describe('DirectorySyncSettings', () => {
  beforeEach(() => {
    vi.resetAllMocks()
  })

  it('renders safe templates and copies an AI prompt with safety guidance', async () => {
    const { wrapper } = await mountDirectorySyncSettings()

    expect(wrapper.text()).toContain('Directory Sync')
    expect(wrapper.text()).toContain('Departments then members')
    expect(wrapper.text()).toContain('Single members endpoint')
    expect(wrapper.text()).toContain('Paged members endpoint')
    expect(wrapper.text()).toContain('directory.example.com')
    expect(wrapper.text()).not.toContain('agoralab')

    await wrapper.get('[data-testid="directory-copy-ai-prompt"]').trigger('click')

    expect(navigator.clipboard.writeText).toHaveBeenCalled()
    const prompt = (navigator.clipboard.writeText as any).mock.calls[0][0]
    expect(prompt).toContain('Do not include real API keys')
    expect(prompt).toContain('directory.example.com')
  })

  it('saves, validates, previews, and runs a directory source', async () => {
    const { wrapper, api } = await mountDirectorySyncSettings()

    await wrapper.get('[data-testid="directory-source-name"]').setValue('Updated Directory')
    await wrapper.get('[data-testid="directory-save"]').trigger('click')
    await flushPromises()
    expect(api.updateDirectorySource).toHaveBeenCalledWith(1, expect.objectContaining({ name: 'Updated Directory' }))

    await wrapper.get('[data-testid="directory-validate"]').trigger('click')
    await flushPromises()
    expect(api.validateDirectorySource).toHaveBeenCalledWith(1)
    expect(wrapper.text()).toContain('Validation passed')

    await wrapper.get('[data-testid="directory-preview"]').trigger('click')
    await flushPromises()
    expect(api.previewDirectorySource).toHaveBeenCalledWith(1)

    await wrapper.get('[data-testid="directory-run-now"]').trigger('click')
    await flushPromises()
    expect(api.startDirectoryRun).toHaveBeenCalledWith(1, { mode: 'apply' })
  })
})
