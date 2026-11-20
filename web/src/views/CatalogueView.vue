<script setup lang="ts">
// CatalogueView lists the catalogue items (GET /api/v1/catalogue-items) and
// creates, edits and soft deletes them through dialogs. The price is typed
// as an amount and sent as integer cents, parsed from the digits without a
// float (see ../money.ts). A duplicate sku answers 409 without details, so
// the view puts its own message on the sku field.
import {
  isApiError,
  useToasts,
  GovueButton,
  GovueConfirmDialog,
  GovueDialog,
  GovueEmptyState,
  GovueErrorState,
  GovueField,
  GovueInput,
  GovuePage,
  GovueSelect,
  GovueSkeleton,
  GovueTextarea,
} from '@ostendorf-cloud/govue'
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { apiClient } from '../api'
import { saveErrorText, translateDetails } from '../fieldErrors'
import { clearSession, sessionReady } from '../session'
import { centsToInput, formatCents, parseCents } from '../money'

interface CatalogueItem {
  id: string
  name: string
  description: string | null
  sku: string | null
  unit_price_cents: number
  unit: string
}

const { t, locale } = useI18n()
const router = useRouter()
const { notify } = useToasts()

const items = ref<CatalogueItem[]>([])
const loading = ref(true)
const loadFailed = ref(false)
const loadRequestId = ref('')

const formOpen = ref(false)
const editingId = ref<string | null>(null)
const form = reactive({ name: '', description: '', sku: '', price: '', unit: 'piece' })
const errors = ref<Record<string, string>>({})
const saving = ref(false)
const saveError = ref('')

const deleteTarget = ref<CatalogueItem | null>(null)
const deleteOpen = ref(false)
const deleting = ref(false)

const unitOptions = computed(() => [
  { value: 'piece', label: t('catalogue.units.piece') },
  { value: 'hour', label: t('catalogue.units.hour') },
  { value: 'day', label: t('catalogue.units.day') },
])

function orNull(value: string): string | null {
  return value.trim() === '' ? null : value
}

async function load(showSkeleton = true): Promise<void> {
  if (showSkeleton) {
    loading.value = true
  }
  loadFailed.value = false
  try {
    const response = await apiClient.get<{ items: CatalogueItem[] }>('/catalogue-items')
    items.value = response.items
  } catch (error) {
    if (isApiError(error) && error.isUnauthenticated) {
      clearSession()
      await router.push({ name: 'login' })
      return
    }
    loadRequestId.value = isApiError(error) ? error.requestId : ''
    loadFailed.value = true
  } finally {
    loading.value = false
  }
}

onMounted(() => load())

function openForm(item: CatalogueItem | null): void {
  editingId.value = item?.id ?? null
  form.name = item?.name ?? ''
  form.description = item?.description ?? ''
  form.sku = item?.sku ?? ''
  form.price = item ? centsToInput(item.unit_price_cents, locale.value) : ''
  form.unit = item?.unit ?? 'piece'
  errors.value = {}
  saveError.value = ''
  formOpen.value = true
}

async function save(): Promise<void> {
  errors.value = {}
  saveError.value = ''
  // The price is required: a blank one would save silently as 0 cents.
  const cents = parseCents(form.price)
  if (cents === null) {
    errors.value = {
      unit_price_cents:
        form.price.trim() === '' ? t('common.fieldErrors.required') : t('catalogue.priceInvalid'),
    }
    saveError.value = t('catalogue.saveError')
    return
  }
  saving.value = true
  const body = {
    name: form.name,
    description: orNull(form.description),
    sku: orNull(form.sku),
    unit_price_cents: cents,
    unit: form.unit,
  }
  try {
    if (editingId.value) {
      await apiClient.put(`/catalogue-items/${editingId.value}`, body)
    } else {
      await apiClient.post('/catalogue-items', body)
    }
    formOpen.value = false
    notify({ kind: 'success', title: t('catalogue.saved') })
    await load(false)
  } catch (error) {
    if (isApiError(error) && error.isUnauthenticated) {
      clearSession()
      await router.push({ name: 'login' })
      return
    }
    if (isApiError(error) && error.status === 400) {
      errors.value = translateDetails(error.details, t)
    } else if (isApiError(error) && error.status === 409) {
      errors.value = { sku: t('catalogue.skuTaken') }
    }
    saveError.value = saveErrorText(error, t, 'catalogue.saveError')
  } finally {
    saving.value = false
  }
}

function askDelete(item: CatalogueItem): void {
  deleteTarget.value = item
  deleteOpen.value = true
}

async function confirmDelete(): Promise<void> {
  if (!deleteTarget.value) {
    return
  }
  deleting.value = true
  try {
    await apiClient.del(`/catalogue-items/${deleteTarget.value.id}`)
    deleteOpen.value = false
    notify({ kind: 'success', title: t('catalogue.deleted') })
    await load(false)
  } catch (error) {
    if (isApiError(error) && error.status === 404) {
      // Already gone: the list is stale, not the delete wrong.
      deleteOpen.value = false
      await load(false)
    } else {
      notify({ kind: 'error', title: t('catalogue.deleteError') })
    }
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <GovuePage width="shell" :title="t('catalogue.title')" :lead="t('catalogue.lead')">
    <div v-if="loading" role="status">
      <span class="sr-only">{{ t('catalogue.loading') }}</span>
      <GovueSkeleton :lines="4" />
    </div>

    <GovueErrorState
      v-else-if="loadFailed"
      :title="t('catalogue.loadErrorTitle')"
      :message="t('catalogue.loadError')"
      :request-id="loadRequestId || undefined"
      @retry="load()"
    />

    <GovueEmptyState
      v-else-if="items.length === 0"
      :title="t('catalogue.emptyTitle')"
      :description="t('catalogue.emptyDescription')"
    >
      <template #action>
        <GovueButton variant="primary" @click="openForm(null)">
          {{ t('catalogue.create') }}
        </GovueButton>
      </template>
    </GovueEmptyState>

    <template v-else>
      <div>
        <GovueButton variant="primary" @click="openForm(null)">
          {{ t('catalogue.create') }}
        </GovueButton>
      </div>

      <div class="overflow-x-auto rounded-lg border border-border bg-surface">
        <table class="w-full text-left text-base">
          <caption class="sr-only">
            {{
              t('catalogue.tableCaption')
            }}
          </caption>
          <thead class="border-b border-border bg-bg-subtle text-sm text-text-muted">
            <tr>
              <th scope="col" class="px-3 py-2 font-medium">{{ t('catalogue.columns.name') }}</th>
              <th scope="col" class="px-3 py-2 font-medium">{{ t('catalogue.columns.sku') }}</th>
              <th scope="col" class="px-3 py-2 font-medium">{{ t('catalogue.columns.unit') }}</th>
              <th scope="col" class="px-3 py-2 text-right font-medium">
                {{ t('catalogue.columns.price') }}
              </th>
              <th scope="col" class="px-3 py-2 font-medium">
                {{ t('catalogue.columns.actions') }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="i in items" :key="i.id" class="border-b border-border last:border-b-0">
              <th scope="row" class="px-3 py-2 font-medium">{{ i.name }}</th>
              <td class="px-3 py-2">{{ i.sku ?? '' }}</td>
              <td class="px-3 py-2">{{ t(`catalogue.units.${i.unit}`) }}</td>
              <td class="px-3 py-2 text-right">{{ formatCents(i.unit_price_cents, locale) }}</td>
              <td class="px-3 py-2">
                <div class="flex flex-wrap gap-2">
                  <GovueButton
                    variant="quiet"
                    :aria-label="t('catalogue.editFor', { name: i.name })"
                    @click="openForm(i)"
                  >
                    {{ t('catalogue.edit') }}
                  </GovueButton>
                  <GovueButton
                    variant="quiet"
                    :aria-label="t('catalogue.deleteFor', { name: i.name })"
                    @click="askDelete(i)"
                  >
                    {{ t('catalogue.delete') }}
                  </GovueButton>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <GovueDialog
      v-model:open="formOpen"
      size="lg"
      :title="editingId ? t('catalogue.editTitle') : t('catalogue.createTitle')"
    >
      <form id="catalogue-form" class="flex flex-col gap-4" novalidate @submit.prevent="save">
        <GovueField :label="t('catalogue.fields.name')" :error="errors.name" required>
          <GovueInput v-model="form.name" autocomplete="off" />
        </GovueField>
        <GovueField :label="t('catalogue.fields.description')" :error="errors.description">
          <GovueTextarea v-model="form.description" :rows="3" />
        </GovueField>
        <GovueField :label="t('catalogue.fields.sku')" :error="errors.sku">
          <GovueInput v-model="form.sku" autocomplete="off" />
        </GovueField>
        <GovueField
          :label="t('catalogue.fields.price')"
          :hint="t('catalogue.fields.priceHint')"
          :error="errors.unit_price_cents"
        >
          <GovueInput v-model="form.price" inputmode="decimal" autocomplete="off" />
        </GovueField>
        <GovueField :label="t('catalogue.fields.unit')" :error="errors.unit">
          <GovueSelect v-model="form.unit" :options="unitOptions" />
        </GovueField>
        <p v-if="saveError" role="alert" class="text-danger-text">{{ saveError }}</p>
      </form>

      <template #actions>
        <GovueButton variant="secondary" :disabled="saving" @click="formOpen = false">
          {{ t('catalogue.cancel') }}
        </GovueButton>
        <GovueButton
          type="submit"
          form="catalogue-form"
          variant="primary"
          :disabled="!sessionReady"
          :loading="saving"
          :loading-label="t('catalogue.saving')"
        >
          {{ t('catalogue.save') }}
        </GovueButton>
      </template>
    </GovueDialog>

    <GovueConfirmDialog
      v-model:open="deleteOpen"
      :title="t('catalogue.deleteTitle')"
      :message="t('catalogue.deleteMessage')"
      :subject="deleteTarget?.name ?? ''"
      :confirm-label="t('catalogue.delete')"
      :cancel-label="t('catalogue.cancel')"
      :loading="deleting"
      @confirm="confirmDelete"
    />
  </GovuePage>
</template>
