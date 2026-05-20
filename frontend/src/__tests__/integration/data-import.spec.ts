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
    showError.mockReset()
    showSuccess.mockReset()
    showWarning.mockReset()
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
})
