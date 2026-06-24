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
  getDirectoryRun: vi.fn(),
}))

Object.assign(navigator, {
  clipboard: {
    writeText: vi.fn(),
  },
})

function warnings(code: string, count: number) {
  return Array.from({ length: count }, () => ({ code, message: code, step_id: 'members' }))
}

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
  api.getDirectoryRun.mockResolvedValue({ data: { data: { id: 10, mode: 'preview', status: 'completed' } } })

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
    expect(prompt).toContain('Use steps[].id')
    expect(prompt).toContain('do not use steps[].name')
    expect(prompt).toContain('department.external_id')
    expect(prompt).toContain('member.email')
    expect(prompt).toContain('Choose member.external_id from a stable non-empty field')
    expect(prompt).toContain('email has complete non-empty coverage')
    expect(prompt).toContain('summarize endpoint, method, root type, item count, field paths, email coverage, and pagination signals')
    expect(prompt).toContain('Do not paste raw response rows')
    expect(prompt).toContain('If you can read docs or call endpoints with tools, do so in read-only mode')
    expect(prompt).toContain('If pagination is not visible in the response body or headers, do not invent it')
    expect(prompt).toContain('Do not output YAML until the required fields are known')
    expect(prompt).toContain('Preserve YAML indentation')
    expect(prompt).toContain('do not flatten nested keys')
    expect(prompt).toContain('Use extract.items: $ only when the response root is the array')
    expect(prompt).toContain('GET /departments returns data.departments')
    expect(prompt).toContain('Use provided production endpoint URLs, field names, and non-secret header names in the final YAML')
    expect(prompt).toContain('Do not include API keys, bearer tokens, passwords')
    expect(prompt).not.toContain('real company domains')
    expect(prompt).not.toContain('real internal URLs')
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

  it('shows validation issue details', async () => {
    const { wrapper, api } = await mountDirectorySyncSettings()
    api.validateDirectorySource.mockResolvedValueOnce({
      data: {
        data: {
          valid: false,
          issues: [
            { path: 'auth.type', message: 'auth.type must be header' },
            { path: 'steps[0].request.url', message: 'url host is required' },
          ],
        },
      },
    })

    await wrapper.get('[data-testid="directory-validate"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Validation found 2 issue(s)')
    expect(wrapper.text()).toContain('auth.type')
    expect(wrapper.text()).toContain('auth.type must be header')
    expect(wrapper.text()).toContain('steps[0].request.url')
    expect(wrapper.text()).toContain('url host is required')
  })

  it('shows the credential ref from the current DSL', async () => {
    const { wrapper } = await mountDirectorySyncSettings()

    await wrapper.get('[data-testid="directory-dsl"]').setValue(`version: 1
scope: full_company
auth:
  type: header
  header: X-Directory-API-Key
  credential_ref: custom_directory_key
`)

    expect(wrapper.text()).toContain('Credential ref: custom_directory_key')
  })

  it('shows preview and apply run failure details', async () => {
    const { wrapper, api } = await mountDirectorySyncSettings()
    api.previewDirectorySource.mockResolvedValueOnce({
      data: { data: { id: 20, mode: 'preview', status: 'failed', error_message: 'steps[0].request.url: url host is required' } },
    })
    api.startDirectoryRun.mockResolvedValueOnce({
      data: { data: { id: 21, mode: 'apply', status: 'failed', error_message: 'steps[0].map: department or member mapping is required' } },
    })

    await wrapper.get('[data-testid="directory-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Preview run failed')
    expect(wrapper.text()).toContain('steps[0].request.url: url host is required')

    await wrapper.get('[data-testid="directory-run-now"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Apply run failed')
    expect(wrapper.text()).toContain('steps[0].map: department or member mapping is required')
  })

  it('shows completed preview and run counts when the backend returns warnings', async () => {
    const { wrapper, api } = await mountDirectorySyncSettings()
    api.previewDirectorySource.mockResolvedValueOnce({
      data: {
        data: {
          id: 30,
          mode: 'preview',
          status: 'completed_with_warnings',
          department_count: 184,
          member_count: 631,
          warning_count: 3759,
          warnings: [...warnings('duplicate_member_email', 3758), ...warnings('invalid_member_email', 1)],
        },
      },
    })
    api.startDirectoryRun.mockResolvedValueOnce({
      data: {
        data: {
          id: 31,
          mode: 'apply',
          status: 'completed_with_warnings',
          department_count: 184,
          member_count: 631,
          warning_count: 3759,
          warnings: [...warnings('duplicate_member_email', 3758), ...warnings('invalid_member_email', 1)],
        },
      },
    })

    await wrapper.get('[data-testid="directory-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Preview completed: kept 631 valid members, skipped 3759 records; 184 departments.')
    expect(wrapper.text()).toContain('Skipped record reasons')
    expect(wrapper.text()).toContain('Duplicate email: 3758 records')
    expect(wrapper.text()).toContain('Missing or invalid email: 1 record')

    await wrapper.get('[data-testid="directory-run-now"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Run completed: kept 631 valid members, skipped 3759 records; 184 departments.')
    expect(wrapper.text()).toContain('Duplicate email: 3758 records')
  })

  it('shows localized completed-with-warnings run counts in Chinese', async () => {
    setLocale('zh-CN')
    const { wrapper, api } = await mountDirectorySyncSettings()
    api.previewDirectorySource.mockResolvedValueOnce({
      data: {
        data: {
          id: 32,
          mode: 'preview',
          status: 'completed_with_warnings',
          department_count: 184,
          member_count: 631,
          warning_count: 3759,
          warnings: [...warnings('duplicate_member_email', 3758), ...warnings('invalid_member_email', 1)],
        },
      },
    })
    api.startDirectoryRun.mockResolvedValueOnce({
      data: {
        data: {
          id: 33,
          mode: 'apply',
          status: 'completed_with_warnings',
          department_count: 184,
          member_count: 631,
          warning_count: 3759,
          warnings: [...warnings('duplicate_member_email', 3758), ...warnings('invalid_member_email', 1)],
        },
      },
    })

    await wrapper.get('[data-testid="directory-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('预览已完成：已保留 631 个有效成员，跳过 3759 条记录；部门 184 个。')
    expect(wrapper.text()).toContain('跳过原因')
    expect(wrapper.text()).toContain('重复邮箱：3758 条')
    expect(wrapper.text()).toContain('缺失或无效邮箱：1 条')
    expect(wrapper.text()).toContain('通常表示同一成员从多个部门结果重复返回')

    await wrapper.get('[data-testid="directory-run-now"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('运行已完成：已保留 631 个有效成员，跳过 3759 条记录；部门 184 个。')
    expect(wrapper.text()).toContain('重复邮箱：3758 条')
  })

  it('polls preview progress until the run completes', async () => {
    vi.useFakeTimers()
    const { wrapper, api } = await mountDirectorySyncSettings()
    api.previewDirectorySource.mockResolvedValueOnce({
      data: { data: { id: 40, mode: 'preview', status: 'queued', phase: 'validating', department_count: 0, member_count: 0, warning_count: 0 } },
    })
    api.getDirectoryRun
      .mockResolvedValueOnce({ data: { data: { id: 40, mode: 'preview', status: 'running', phase: 'executing', department_count: 0, member_count: 0, warning_count: 0 } } })
      .mockResolvedValueOnce({ data: { data: { id: 40, mode: 'preview', status: 'completed', phase: 'completed', department_count: 2, member_count: 3, warning_count: 0 } } })

    try {
      await wrapper.get('[data-testid="directory-preview"]').trigger('click')
      expect(wrapper.text()).toContain('Starting preview...')
      await flushPromises()
      expect(wrapper.text()).toContain('Reading directory API')
      expect(api.getDirectoryRun).toHaveBeenCalledWith(40)

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()
      expect(wrapper.text()).toContain('Preview completed: kept 3 valid members; 2 departments.')
      expect(wrapper.text()).not.toContain('Reading directory API')
    } finally {
      vi.useRealTimers()
    }
  })

  it('shows localized apply progress while polling the run', async () => {
    vi.useFakeTimers()
    setLocale('zh-CN')
    const { wrapper, api } = await mountDirectorySyncSettings()
    api.startDirectoryRun.mockResolvedValueOnce({
      data: { data: { id: 41, mode: 'apply', status: 'queued', phase: 'validating', department_count: 0, member_count: 0, warning_count: 0 } },
    })
    api.getDirectoryRun
      .mockResolvedValueOnce({ data: { data: { id: 41, mode: 'apply', status: 'running', phase: 'executing', department_count: 0, member_count: 0, warning_count: 0 } } })
      .mockResolvedValueOnce({
        data: {
          data: {
            id: 41,
            mode: 'apply',
            status: 'completed_with_warnings',
            phase: 'completed',
            department_count: 184,
            member_count: 633,
            warning_count: 3769,
            warnings: warnings('duplicate_member_email', 3769),
          },
        },
      })

    try {
      await wrapper.get('[data-testid="directory-run-now"]').trigger('click')
      expect(wrapper.text()).toContain('正在启动运行...')
      await flushPromises()
      expect(wrapper.text()).toContain('正在读取组织架构接口')
      expect(api.getDirectoryRun).toHaveBeenCalledWith(41)

      await vi.runOnlyPendingTimersAsync()
      await flushPromises()
      expect(wrapper.text()).toContain('运行已完成：已保留 633 个有效成员，跳过 3769 条记录；部门 184 个。')
      expect(wrapper.text()).toContain('重复邮箱：3769 条')
      expect(wrapper.text()).not.toContain('正在读取组织架构接口')
    } finally {
      vi.useRealTimers()
    }
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
    expect(prompt).toContain('使用 steps[].id')
    expect(prompt).toContain('不要使用 steps[].name')
    expect(prompt).toContain('只读方式读取文档或调用接口')
    expect(prompt).toContain('不要粘贴原始响应行')
    expect(prompt).toContain('邮箱字段必须有完整的非空覆盖')
    expect(prompt).toContain('不要编造分页')
    expect(prompt).toContain('保留 YAML 缩进')
    expect(prompt).toContain('不要把嵌套字段拉平成顶层字段')
    expect(prompt).toContain('只有响应根本身就是数组时才使用 extract.items: $')
    expect(prompt).toContain('最终 YAML 可以使用配置者提供的生产接口 URL、字段名和非密钥 header 名称')
    expect(prompt).toContain('不要包含 API Key、bearer token、密码')
    expect(prompt).toContain('directory.example.com')
  })
})
