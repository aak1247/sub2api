import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ImportDataModal from '@/components/admin/account/ImportDataModal.vue'

const showError = vi.fn()
const showSuccess = vi.fn()
const showWarning = vi.fn()

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showWarning
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      importData: vi.fn(),
      searchData: vi.fn()
    }
  }
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

describe('ImportDataModal', () => {
  beforeEach(() => {
    vi.resetAllMocks()
  })

  it('未选择文件时提示错误', async () => {
    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    await wrapper.find('form').trigger('submit')
    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportSelectFile')
  })

  it('无效 JSON 时提示解析失败', async () => {
    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    const input = wrapper.find('input[type="file"]')
    const file = new File(['invalid json'], 'data.json', { type: 'application/json' })
    Object.defineProperty(file, 'text', {
      value: () => Promise.resolve('invalid json')
    })
    Object.defineProperty(input.element, 'files', {
      value: [file]
    })

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await Promise.resolve()

    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportParseFailed')
  })

  it('搜索按钮会筛选现有账号但不导入', async () => {
    const { adminAPI } = await import('@/api/admin')
    const searchMock = adminAPI.accounts.searchData as ReturnType<typeof vi.fn>
    searchMock.mockResolvedValue({
      account_candidates: 1,
      account_matched: 1,
      account_failed: 0,
      accounts: [
        {
          id: 101,
          name: 'acc',
          platform: 'openai',
          type: 'oauth',
          status: 'active'
        }
      ],
      errors: []
    })

    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    const input = wrapper.find('input[type="file"]')
    const file = new File([
      JSON.stringify({
        type: 'sub2api-data',
        version: 1,
        proxies: [],
        accounts: []
      })
    ], 'data.json', { type: 'application/json' })
    Object.defineProperty(file, 'text', {
      value: () => Promise.resolve(JSON.stringify({ type: 'sub2api-data', version: 1, proxies: [], accounts: [] }))
    })
    Object.defineProperty(input.element, 'files', {
      value: [file]
    })

    await input.trigger('change')
    const searchButton = wrapper.findAll('button').find((button) => button.text() === 'admin.accounts.dataSearchButton')
    expect(searchButton).toBeTruthy()
    await searchButton!.trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(searchMock).toHaveBeenCalled()
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.dataSearchSuccess')
    expect(showError).not.toHaveBeenCalledWith('admin.accounts.dataImportParseFailed')
  })

  it('导入完整请求体格式时会提取内层 data 作为数据载荷', async () => {
    const { adminAPI } = await import('@/api/admin')
    const importMock = adminAPI.accounts.importData as ReturnType<typeof vi.fn>
    importMock.mockResolvedValue({
      proxy_created: 0,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 1,
      account_updated: 0,
      account_failed: 0,
      errors: []
    })

    const payload = {
      type: 'sub2api-data',
      version: 1,
      exported_at: '2026-06-01T09:37:10Z',
      proxies: [],
      accounts: [
        {
          name: 'acc',
          platform: 'openai',
          type: 'oauth',
          credentials: { access_token: 'token' },
          extra: { email: 'acc@example.com' },
          concurrency: 10,
          priority: 1,
          rate_multiplier: 1,
          auto_pause_on_expired: true
        }
      ]
    }
    const requestPayload = {
      data: payload,
      skip_default_group_bind: true
    }
    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    const input = wrapper.find('input[type="file"]')
    const file = new File([JSON.stringify(requestPayload)], 'data.json', { type: 'application/json' })
    Object.defineProperty(file, 'text', {
      value: () => Promise.resolve(JSON.stringify(requestPayload))
    })
    Object.defineProperty(input.element, 'files', {
      value: [file]
    })

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(importMock).toHaveBeenCalledWith({
      data: payload,
      skip_default_group_bind: true,
      update_existing: false,
      duplicate_account_mode: 'skip'
    })
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.dataImportSuccess')
  })

  it('重复账号错误结果显示覆盖导入按钮并带 update_existing 重试', async () => {
    const { adminAPI } = await import('@/api/admin')
    const importMock = adminAPI.accounts.importData as ReturnType<typeof vi.fn>
    importMock
      .mockResolvedValueOnce({
        proxy_created: 0,
        proxy_reused: 0,
        proxy_failed: 0,
        account_created: 0,
        account_updated: 0,
        account_failed: 1,
        errors: [
          {
            kind: 'account',
            name: 'existing',
            message: 'duplicate account already exists: #101 existing'
          }
        ]
      })
      .mockResolvedValueOnce({
        proxy_created: 0,
        proxy_reused: 0,
        proxy_failed: 0,
        account_created: 0,
        account_updated: 1,
        account_failed: 0,
        errors: []
      })

    const payload = { type: 'sub2api-data', version: 1, proxies: [], accounts: [] }
    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    const input = wrapper.find('input[type="file"]')
    const file = new File([JSON.stringify(payload)], 'data.json', { type: 'application/json' })
    Object.defineProperty(file, 'text', {
      value: () => Promise.resolve(JSON.stringify(payload))
    })
    Object.defineProperty(input.element, 'files', {
      value: [file]
    })

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await new Promise((resolve) => setTimeout(resolve, 0))

    const overwriteButton = wrapper.findAll('button').find((button) => button.text() === 'admin.accounts.dataImportOverwriteButton')
    expect(overwriteButton).toBeTruthy()
    expect(importMock).toHaveBeenCalledWith({
      data: payload,
      skip_default_group_bind: true,
      update_existing: false,
      duplicate_account_mode: 'skip'
    })

    await overwriteButton!.trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(importMock).toHaveBeenLastCalledWith({
      data: payload,
      skip_default_group_bind: true,
      update_existing: true,
      duplicate_account_mode: 'overwrite'
    })
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.dataImportSuccess')
  })

  it('重复账号错误结果显示并存导入按钮并带 coexist 模式重试', async () => {
    const { adminAPI } = await import('@/api/admin')
    const importMock = adminAPI.accounts.importData as ReturnType<typeof vi.fn>
    importMock
      .mockResolvedValueOnce({
        proxy_created: 0,
        proxy_reused: 0,
        proxy_failed: 0,
        account_created: 0,
        account_updated: 0,
        account_failed: 1,
        errors: [
          {
            kind: 'account',
            name: 'existing',
            message: 'duplicate account already exists: #101 existing'
          }
        ]
      })
      .mockResolvedValueOnce({
        proxy_created: 0,
        proxy_reused: 0,
        proxy_failed: 0,
        account_created: 1,
        account_updated: 0,
        account_failed: 0,
        errors: []
      })

    const payload = { type: 'sub2api-data', version: 1, proxies: [], accounts: [] }
    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    const input = wrapper.find('input[type="file"]')
    const file = new File([JSON.stringify(payload)], 'data.json', { type: 'application/json' })
    Object.defineProperty(file, 'text', {
      value: () => Promise.resolve(JSON.stringify(payload))
    })
    Object.defineProperty(input.element, 'files', {
      value: [file]
    })

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await new Promise((resolve) => setTimeout(resolve, 0))

    const coexistButton = wrapper.findAll('button').find((button) => button.text() === 'admin.accounts.dataImportCoexistButton')
    expect(coexistButton).toBeTruthy()

    await coexistButton!.trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(importMock).toHaveBeenLastCalledWith({
      data: payload,
      skip_default_group_bind: true,
      update_existing: false,
      duplicate_account_mode: 'coexist'
    })
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.dataImportSuccess')
  })
})
