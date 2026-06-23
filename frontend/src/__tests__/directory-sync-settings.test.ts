import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import DirectorySyncSettings from '@/components/settings/DirectorySyncSettings.vue'
import { setLocale } from '@/i18n'

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
    setLocale('en-US')
  })

  it('renders safe templates and copies an AI prompt with safety guidance', async () => {
    const { wrapper } = await mountDirectorySyncSettings()

    expect(wrapper.text()).toContain('Directory Sync')
    expect(wrapper.text()).toContain('Departments then members')
    expect(wrapper.text()).toContain('Single members endpoint')
    expect(wrapper.text()).toContain('Paged members endpoint')
    expect(wrapper.text()).toContain('API documentation or endpoint notes')
    expect(wrapper.text()).toContain('If there is no API documentation, provide these interfaces one by one')
    expect(wrapper.text()).toContain('directory.example.com')

    await wrapper.get('[data-testid="directory-ai-context"]').setValue('GET /departments returns data.departments; GET /users returns data.users.')
    await wrapper.get('[data-testid="directory-copy-ai-prompt"]').trigger('click')

    expect(navigator.clipboard.writeText).toHaveBeenCalled()
    const prompt = (navigator.clipboard.writeText as any).mock.calls[0][0]
    expect(prompt).toContain('Target YAML contract')
    expect(prompt).toContain('Normalized output structures')
    expect(prompt).toContain('department.external_id')
    expect(prompt).toContain('member.email')
    expect(prompt).toContain('GET /departments returns data.departments')
    expect(prompt).toContain('Do not include real API keys')
    expect(prompt).toContain('directory.example.com')
  })

  it('copies a step-by-step prompt when API documentation is missing', async () => {
    const { wrapper } = await mountDirectorySyncSettings()

    await wrapper.get('[data-testid="directory-copy-ai-prompt"]').trigger('click')

    const prompt = (navigator.clipboard.writeText as any).mock.calls[0][0]
    expect(prompt).toContain('No API documentation was provided')
    expect(prompt).toContain('Ask the configurator for each interface in this order')
    expect(prompt).toContain('1. Department list endpoint')
    expect(prompt).toContain('2. Member list endpoint')
    expect(prompt).toContain('3. Pagination')
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

  it('switches directory sync copy to Chinese', async () => {
    setLocale('zh-CN')
    const { wrapper } = await mountDirectorySyncSettings()

    expect(wrapper.text()).toContain('组织架构同步')
    expect(wrapper.text()).toContain('先部门后成员')
    expect(wrapper.text()).toContain('复制 AI Prompt')
    expect(wrapper.text()).toContain('接口文档或接口说明')
    expect(wrapper.text()).toContain('没有接口文档时，逐项提供这些接口')

    await wrapper.get('[data-testid="directory-copy-ai-prompt"]').trigger('click')
    const prompt = (navigator.clipboard.writeText as any).mock.calls[0][0]
    expect(prompt).toContain('目标 YAML 合同')
    expect(prompt).toContain('标准化输出结构')
    expect(prompt).toContain('不要包含真实 API Key')
    expect(prompt).toContain('directory.example.com')
  })
})
