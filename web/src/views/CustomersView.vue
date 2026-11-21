<script setup lang="ts">
// CustomersView lists the customers (GET /api/v1/customers) and creates,
// edits and soft deletes them through dialogs. A field error from the API
// `details` lands on its field. A duplicate customer number answers 409
// without details, so the view puts its own message on the number field.
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
} from '@ostendorf-cloud/govue'
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { apiClient } from '../api'
import { saveErrorText, translateDetails } from '../fieldErrors'
import { clearSession, loadSession, sessionFailed, sessionReady } from '../session'

interface Customer {
  id: string
  name: string
  number: string | null
  email: string | null
  vat_id: string | null
  street: string | null
  address_line2: string | null
  postal_code: string | null
  city: string | null
  country_code: string | null
  document_language: string
}

const { t } = useI18n()
const router = useRouter()
const { notify } = useToasts()

const customers = ref<Customer[]>([])
const loading = ref(true)
const loadFailed = ref(false)
const loadRequestId = ref('')

const formOpen = ref(false)
const editingId = ref<string | null>(null)
const form = reactive({
  name: '',
  number: '',
  email: '',
  vat_id: '',
  street: '',
  address_line2: '',
  postal_code: '',
  city: '',
  country_code: '',
  document_language: 'de',
})
const errors = ref<Record<string, string>>({})
const saving = ref(false)
const saveError = ref('')

const deleteTarget = ref<Customer | null>(null)
const deleteOpen = ref(false)
const deleting = ref(false)

const languageOptions = computed(() => [
  { value: 'de', label: t('customers.language.de') },
  { value: 'en', label: t('customers.language.en') },
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
    const response = await apiClient.get<{ customers: Customer[] }>('/customers')
    customers.value = response.customers
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

function openForm(customer: Customer | null): void {
  editingId.value = customer?.id ?? null
  form.name = customer?.name ?? ''
  form.number = customer?.number ?? ''
  form.email = customer?.email ?? ''
  form.vat_id = customer?.vat_id ?? ''
  form.street = customer?.street ?? ''
  form.address_line2 = customer?.address_line2 ?? ''
  form.postal_code = customer?.postal_code ?? ''
  form.city = customer?.city ?? ''
  form.country_code = customer?.country_code ?? ''
  form.document_language = customer?.document_language ?? 'de'
  errors.value = {}
  saveError.value = ''
  formOpen.value = true
  // The mutating calls need the CSRF token from /me: try once more when the
  // shell's own load failed.
  if (!sessionReady.value) {
    void loadSession()
  }
}

async function save(): Promise<void> {
  saving.value = true
  saveError.value = ''
  errors.value = {}
  const body = {
    name: form.name,
    number: orNull(form.number),
    email: orNull(form.email),
    vat_id: orNull(form.vat_id),
    street: orNull(form.street),
    address_line2: orNull(form.address_line2),
    postal_code: orNull(form.postal_code),
    city: orNull(form.city),
    country_code: orNull(form.country_code),
    document_language: form.document_language,
  }
  try {
    if (editingId.value) {
      await apiClient.put(`/customers/${editingId.value}`, body)
    } else {
      await apiClient.post('/customers', body)
    }
    formOpen.value = false
    notify({ kind: 'success', title: t('customers.saved') })
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
      errors.value = { number: t('customers.numberTaken') }
    }
    saveError.value = saveErrorText(error, t, 'customers.saveError')
  } finally {
    saving.value = false
  }
}

function askDelete(customer: Customer): void {
  deleteTarget.value = customer
  deleteOpen.value = true
}

async function confirmDelete(): Promise<void> {
  if (!deleteTarget.value) {
    return
  }
  if (!sessionReady.value) {
    // No CSRF token yet: the request would only be refused.
    notify({ kind: 'error', title: t('common.errors.session') })
    void loadSession()
    return
  }
  deleting.value = true
  try {
    await apiClient.del(`/customers/${deleteTarget.value.id}`)
    deleteOpen.value = false
    notify({ kind: 'success', title: t('customers.deleted') })
    await load(false)
  } catch (error) {
    if (isApiError(error) && error.isUnauthenticated) {
      clearSession()
      deleteOpen.value = false
      await router.push({ name: 'login' })
      return
    }
    if (isApiError(error) && error.status === 404) {
      // Already gone: the list is stale, not the delete wrong.
      deleteOpen.value = false
      await load(false)
    } else {
      notify({ kind: 'error', title: t('customers.deleteError') })
    }
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <GovuePage width="shell" :title="t('customers.title')" :lead="t('customers.lead')">
    <div v-if="loading" role="status">
      <span class="sr-only">{{ t('customers.loading') }}</span>
      <GovueSkeleton :lines="4" />
    </div>

    <GovueErrorState
      v-else-if="loadFailed"
      :title="t('customers.loadErrorTitle')"
      :message="t('customers.loadError')"
      :request-id="loadRequestId || undefined"
      @retry="load()"
    />

    <GovueEmptyState
      v-else-if="customers.length === 0"
      :title="t('customers.emptyTitle')"
      :description="t('customers.emptyDescription')"
    >
      <template #action>
        <GovueButton variant="primary" @click="openForm(null)">
          {{ t('customers.create') }}
        </GovueButton>
      </template>
    </GovueEmptyState>

    <template v-else>
      <div>
        <GovueButton variant="primary" @click="openForm(null)">
          {{ t('customers.create') }}
        </GovueButton>
      </div>

      <div class="overflow-x-auto rounded-lg border border-border bg-surface">
        <table class="w-full text-left text-base">
          <caption class="sr-only">
            {{
              t('customers.tableCaption')
            }}
          </caption>
          <thead class="border-b border-border bg-bg-subtle text-sm text-text-muted">
            <tr>
              <th scope="col" class="px-3 py-2 font-medium">{{ t('customers.columns.number') }}</th>
              <th scope="col" class="px-3 py-2 font-medium">{{ t('customers.columns.name') }}</th>
              <th scope="col" class="px-3 py-2 font-medium">{{ t('customers.columns.city') }}</th>
              <th scope="col" class="px-3 py-2 font-medium">
                {{ t('customers.columns.language') }}
              </th>
              <th scope="col" class="px-3 py-2 font-medium">
                {{ t('customers.columns.actions') }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="c in customers" :key="c.id" class="border-b border-border last:border-b-0">
              <td class="px-3 py-2">{{ c.number ?? '' }}</td>
              <th scope="row" class="px-3 py-2 font-medium">{{ c.name }}</th>
              <td class="px-3 py-2">{{ c.city ?? '' }}</td>
              <td class="px-3 py-2">{{ t(`customers.language.${c.document_language}`) }}</td>
              <td class="px-3 py-2">
                <div class="flex flex-wrap gap-2">
                  <GovueButton
                    variant="quiet"
                    :aria-label="t('customers.editFor', { name: c.name })"
                    @click="openForm(c)"
                  >
                    {{ t('customers.edit') }}
                  </GovueButton>
                  <GovueButton
                    variant="quiet"
                    :aria-label="t('customers.deleteFor', { name: c.name })"
                    @click="askDelete(c)"
                  >
                    {{ t('customers.delete') }}
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
      :title="editingId ? t('customers.editTitle') : t('customers.createTitle')"
    >
      <form id="customer-form" class="flex flex-col gap-4" novalidate @submit.prevent="save">
        <GovueField :label="t('customers.fields.name')" :error="errors.name" required>
          <GovueInput v-model="form.name" autocomplete="off" />
        </GovueField>
        <GovueField :label="t('customers.fields.number')" :error="errors.number">
          <GovueInput v-model="form.number" autocomplete="off" />
        </GovueField>
        <GovueField :label="t('customers.fields.email')" :error="errors.email">
          <GovueInput v-model="form.email" type="email" autocomplete="off" />
        </GovueField>
        <GovueField :label="t('customers.fields.vatId')" :error="errors.vat_id">
          <GovueInput v-model="form.vat_id" autocomplete="off" />
        </GovueField>
        <GovueField :label="t('customers.fields.street')" :error="errors.street">
          <GovueInput v-model="form.street" autocomplete="off" />
        </GovueField>
        <GovueField :label="t('customers.fields.addressLine2')" :error="errors.address_line2">
          <GovueInput v-model="form.address_line2" autocomplete="off" />
        </GovueField>
        <GovueField :label="t('customers.fields.postalCode')" :error="errors.postal_code">
          <GovueInput v-model="form.postal_code" autocomplete="off" />
        </GovueField>
        <GovueField :label="t('customers.fields.city')" :error="errors.city">
          <GovueInput v-model="form.city" autocomplete="off" />
        </GovueField>
        <GovueField
          :label="t('customers.fields.countryCode')"
          :hint="t('customers.fields.countryCodeHint')"
          :error="errors.country_code"
        >
          <GovueInput v-model="form.country_code" autocomplete="off" />
        </GovueField>
        <GovueField
          :label="t('customers.fields.documentLanguage')"
          :error="errors.document_language"
        >
          <GovueSelect v-model="form.document_language" :options="languageOptions" />
        </GovueField>
        <p v-if="saveError" role="alert" class="text-danger-text">{{ saveError }}</p>
        <p v-if="sessionFailed && !sessionReady" role="alert" class="text-danger-text">
          {{ t('common.errors.session') }}
        </p>
      </form>

      <template #actions>
        <GovueButton variant="secondary" :disabled="saving" @click="formOpen = false">
          {{ t('customers.cancel') }}
        </GovueButton>
        <GovueButton
          type="submit"
          form="customer-form"
          variant="primary"
          :disabled="!sessionReady"
          :loading="saving"
          :loading-label="t('customers.saving')"
        >
          {{ t('customers.save') }}
        </GovueButton>
      </template>
    </GovueDialog>

    <GovueConfirmDialog
      v-model:open="deleteOpen"
      :title="t('customers.deleteTitle')"
      :message="t('customers.deleteMessage')"
      :subject="deleteTarget?.name ?? ''"
      :confirm-label="t('customers.delete')"
      :cancel-label="t('customers.cancel')"
      :loading="deleting"
      @confirm="confirmDelete"
    />
  </GovuePage>
</template>
