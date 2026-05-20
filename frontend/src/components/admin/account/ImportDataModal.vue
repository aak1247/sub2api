<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.dataImportTitle')"
    width="normal"
    close-on-click-outside
    @close="handleClose"
  >
    <form id="import-data-form" class="space-y-4" @submit.prevent="handleImport">
      <div class="text-sm text-gray-600 dark:text-dark-300">
        {{ t('admin.accounts.dataImportHint') }}
      </div>
      <div
        class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-600 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-400"
      >
        {{ t('admin.accounts.dataImportWarning') }}
      </div>

      <div>
        <label class="input-label">{{ t('admin.accounts.dataImportFile') }}</label>
        <div
          class="flex items-center justify-between gap-3 rounded-lg border border-dashed border-gray-300 bg-gray-50 px-4 py-3 dark:border-dark-600 dark:bg-dark-800"
        >
          <div class="min-w-0">
            <div class="truncate text-sm text-gray-700 dark:text-dark-200">
              {{ fileName || t('admin.accounts.dataImportSelectFile') }}
            </div>
            <div class="text-xs text-gray-500 dark:text-dark-400">JSON (.json)</div>
          </div>
          <button type="button" class="btn btn-secondary shrink-0" @click="openFilePicker">
            {{ t('common.chooseFile') }}
          </button>
        </div>
        <input
          ref="fileInput"
          type="file"
          class="hidden"
          accept="application/json,.json"
          @change="handleFileChange"
        />
      </div>

      <div
        v-if="searchResult"
        class="space-y-2 rounded-xl border border-blue-200 p-4 dark:border-blue-900/60"
      >
        <div class="text-sm font-medium text-gray-900 dark:text-white">
          {{ t('admin.accounts.dataSearchResult') }}
        </div>
        <div class="text-sm text-gray-700 dark:text-dark-300">
          {{ t('admin.accounts.dataSearchResultSummary', searchResult) }}
        </div>

        <div v-if="searchAccounts.length" class="mt-2">
          <div class="text-sm font-medium text-gray-700 dark:text-dark-200">
            {{ t('admin.accounts.dataSearchAccounts') }}
          </div>
          <div class="mt-2 max-h-48 overflow-auto rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-800">
            <div v-for="(item, idx) in searchAccounts" :key="idx" class="whitespace-pre-wrap">
              #{{ item.id }} · {{ item.name }} · {{ item.platform }} · {{ item.type }}
            </div>
          </div>
        </div>

        <div v-if="searchErrors.length" class="mt-2">
          <div class="text-sm font-medium text-red-600 dark:text-red-400">
            {{ t('admin.accounts.dataSearchErrors') }}
          </div>
          <div class="mt-2 max-h-48 overflow-auto rounded-lg bg-gray-50 p-3 font-mono text-xs dark:bg-dark-800">
            <div v-for="(item, idx) in searchErrors" :key="idx" class="whitespace-pre-wrap">
              {{ item.kind }} {{ item.name || item.proxy_key || '-' }} — {{ item.message }}
            </div>
          </div>
        </div>
      </div>

      <div
        v-if="importResult"
        class="space-y-2 rounded-xl border border-gray-200 p-4 dark:border-dark-700"
      >
        <div class="text-sm font-medium text-gray-900 dark:text-white">
          {{ t('admin.accounts.dataImportResult') }}
        </div>
        <div class="text-sm text-gray-700 dark:text-dark-300">
          {{ t('admin.accounts.dataImportResultSummary', importResult) }}
        </div>

        <div v-if="importErrors.length" class="mt-2">
          <div class="text-sm font-medium text-red-600 dark:text-red-400">
            {{ t('admin.accounts.dataImportErrors') }}
          </div>
          <div class="mt-2 max-h-48 overflow-auto rounded-lg bg-gray-50 p-3 font-mono text-xs dark:bg-dark-800">
            <div v-for="(item, idx) in importErrors" :key="idx" class="whitespace-pre-wrap">
              {{ item.kind }} {{ item.name || item.proxy_key || '-' }} — {{ item.message }}
            </div>
          </div>
        </div>
      </div>
    </form>

    <template #footer>
      <div class="flex w-full items-center justify-between gap-3">
        <button class="btn btn-secondary" type="button" :disabled="busy" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <div class="flex gap-3">
          <button class="btn btn-secondary" type="button" :disabled="busy" @click="handleSearch">
            {{ searching ? t('admin.accounts.dataSearching') : t('admin.accounts.dataSearchButton') }}
          </button>
          <button
            class="btn btn-primary"
            type="submit"
            form="import-data-form"
            :disabled="busy"
          >
            {{ importing ? t('admin.accounts.dataImporting') : t('admin.accounts.dataImportButton') }}
          </button>
        </div>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { Account, AdminDataImportResult, AdminDataSearchResult } from '@/types'

interface Props {
  show: boolean
}

interface Emits {
  (e: 'close'): void
  (e: 'imported'): void
  (e: 'searched', accounts: Account[]): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const { t } = useI18n()
const appStore = useAppStore()

const importing = ref(false)
const searching = ref(false)
const file = ref<File | null>(null)
const importResult = ref<AdminDataImportResult | null>(null)
const searchResult = ref<AdminDataSearchResult | null>(null)

const fileInput = ref<HTMLInputElement | null>(null)
const fileName = computed(() => file.value?.name || '')
const busy = computed(() => importing.value || searching.value)
const importErrors = computed(() => importResult.value?.errors || [])
const searchAccounts = computed(() => searchResult.value?.accounts || [])
const searchErrors = computed(() => searchResult.value?.errors || [])

watch(
  () => props.show,
  (open) => {
    if (open) {
      file.value = null
      importResult.value = null
      searchResult.value = null
      if (fileInput.value) {
        fileInput.value.value = ''
      }
    }
  }
)

const openFilePicker = () => {
  fileInput.value?.click()
}

const handleFileChange = (event: Event) => {
  const target = event.target as HTMLInputElement
  file.value = target.files?.[0] || null
}

const handleClose = () => {
  if (busy.value) return
  emit('close')
}

const readFileAsText = async (sourceFile: File): Promise<string> => {
  if (typeof sourceFile.text === 'function') {
    return sourceFile.text()
  }

  if (typeof sourceFile.arrayBuffer === 'function') {
    const buffer = await sourceFile.arrayBuffer()
    return new TextDecoder().decode(buffer)
  }

  return await new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result ?? ''))
    reader.onerror = () => reject(reader.error || new Error('Failed to read file'))
    reader.readAsText(sourceFile)
  })
}

const readPayload = async () => {
  if (!file.value) {
    throw new Error('file_required')
  }
  const text = await readFileAsText(file.value)
  return JSON.parse(text)
}

const handleSearch = async () => {
  if (!file.value) {
    appStore.showError(t('admin.accounts.dataImportSelectFile'))
    return
  }

  searching.value = true
  try {
    const dataPayload = await readPayload()
    const res = await adminAPI.accounts.searchData({ data: dataPayload })
    searchResult.value = res
    emit('searched', res.accounts || [])

    const msgParams: Record<string, unknown> = {
      account_candidates: res.account_candidates,
      account_matched: res.account_matched,
      account_failed: res.account_failed,
    }
    if (res.account_failed > 0) {
      appStore.showWarning(t('admin.accounts.dataSearchCompletedWithErrors', msgParams))
    } else {
      appStore.showSuccess(t('admin.accounts.dataSearchSuccess', msgParams))
    }
  } catch (error: any) {
    if (error instanceof SyntaxError) {
      appStore.showError(t('admin.accounts.dataImportParseFailed'))
    } else {
      appStore.showError(error?.message || t('admin.accounts.dataImportFailed'))
    }
  } finally {
    searching.value = false
  }
}

const handleImport = async () => {
  if (!file.value) {
    appStore.showError(t('admin.accounts.dataImportSelectFile'))
    return
  }

  importing.value = true
  try {
    const dataPayload = await readPayload()

    const res = await adminAPI.accounts.importData({
      data: dataPayload,
      skip_default_group_bind: true
    })

    importResult.value = res

    const msgParams: Record<string, unknown> = {
      account_created: res.account_created,
      account_failed: res.account_failed,
      proxy_created: res.proxy_created,
      proxy_reused: res.proxy_reused,
      proxy_failed: res.proxy_failed,
    }
    if (res.account_failed > 0 || res.proxy_failed > 0) {
      appStore.showError(t('admin.accounts.dataImportCompletedWithErrors', msgParams))
    } else {
      appStore.showSuccess(t('admin.accounts.dataImportSuccess', msgParams))
      emit('imported')
    }
  } catch (error: any) {
    if (error instanceof SyntaxError) {
      appStore.showError(t('admin.accounts.dataImportParseFailed'))
    } else {
      appStore.showError(error?.message || t('admin.accounts.dataImportFailed'))
    }
  } finally {
    importing.value = false
  }
}
</script>
