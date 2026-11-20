<script setup lang="ts">
// SettingsView is the seller profile (GET and PUT /api/v1/profile, owner
// only) and the VAT regime history (GET and POST /api/v1/vat-regimes). The
// profile holds the supplier data section 14 UStG needs on an invoice. A
// save may be incomplete: completeness is checked when a document is
// issued, so this view only shows which regimes the profile is ready for.
// A period of the history is never edited: a wrong one is corrected by a
// new one that names it.
import {
  isApiError,
  GovueBadge,
  GovueButton,
  GovueCard,
  GovueField,
  GovueInput,
  GovuePage,
  GovueSelect,
  GovueTextarea,
} from '@ostendorf-cloud/govue'
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { apiClient, csrfToken } from '../api'

interface MeResponse {
  role: string
  csrf_token: string
}

interface ProfileResponse {
  legal_name: string
  street: string
  address_line2: string | null
  postal_code: string
  city: string
  country_code: string
  tax_number: string | null
  vat_id: string | null
  kleinunternehmer_id: string | null
  default_vat_regime: string
  default_language: string
  bank_name: string | null
  iban: string | null
  bic: string | null
  email: string | null
  phone: string | null
  website: string | null
  footer_text: string | null
  number_formats: { invoice: string; offer: string }
  issue_ready: Record<string, boolean>
}

interface PeriodResponse {
  id: string
  valid_from: string
  regime: string
  note: string | null
  supersedes_id: string | null
  superseded: boolean
}

const { t } = useI18n()
const router = useRouter()

const loading = ref(true)
const loadError = ref('')
const isOwner = ref(false)

const form = reactive({
  legal_name: '',
  street: '',
  address_line2: '',
  postal_code: '',
  city: '',
  country_code: 'DE',
  tax_number: '',
  vat_id: '',
  kleinunternehmer_id: '',
  default_vat_regime: 'kleinunternehmer',
  default_language: 'de',
  bank_name: '',
  iban: '',
  bic: '',
  email: '',
  phone: '',
  website: '',
  footer_text: '',
  invoice_format: 'RE-{year}{seq:4}',
  offer_format: 'AN-{year}{seq:4}',
})
const issueReady = ref<Record<string, boolean>>({})
const saving = ref(false)
const saved = ref(false)
const saveError = ref('')
const errors = ref<Record<string, string>>({})

const regimeOptions = computed(() => [
  { value: 'kleinunternehmer', label: t('profile.regime.kleinunternehmer') },
  { value: 'standard', label: t('profile.regime.standard') },
])
const languageOptions = computed(() => [
  { value: 'de', label: t('profile.language.de') },
  { value: 'en', label: t('profile.language.en') },
])

const periods = ref<PeriodResponse[]>([])
const period = reactive({
  valid_from: '',
  regime: 'kleinunternehmer',
  note: '',
  supersedes: 'none',
})
const addingPeriod = ref(false)
const periodError = ref('')
const periodErrors = ref<Record<string, string>>({})

const supersedeOptions = computed(() => [
  { value: 'none', label: t('profile.vat.supersedesNone') },
  ...periods.value
    .filter((p) => !p.superseded)
    .map((p) => ({
      value: p.id,
      label: `${p.valid_from} ${t(`profile.regime.${p.regime}`)}`,
    })),
])

function orNull(value: string): string | null {
  return value.trim() === '' ? null : value
}

function fill(p: ProfileResponse): void {
  form.legal_name = p.legal_name
  form.street = p.street
  form.address_line2 = p.address_line2 ?? ''
  form.postal_code = p.postal_code
  form.city = p.city
  form.country_code = p.country_code
  form.tax_number = p.tax_number ?? ''
  form.vat_id = p.vat_id ?? ''
  form.kleinunternehmer_id = p.kleinunternehmer_id ?? ''
  form.default_vat_regime = p.default_vat_regime
  form.default_language = p.default_language
  form.bank_name = p.bank_name ?? ''
  form.iban = p.iban ?? ''
  form.bic = p.bic ?? ''
  form.email = p.email ?? ''
  form.phone = p.phone ?? ''
  form.website = p.website ?? ''
  form.footer_text = p.footer_text ?? ''
  form.invoice_format = p.number_formats.invoice
  form.offer_format = p.number_formats.offer
  issueReady.value = p.issue_ready
}

async function loadPeriods(): Promise<void> {
  const response = await apiClient.get<{ periods: PeriodResponse[] }>('/vat-regimes')
  periods.value = response.periods
}

onMounted(async () => {
  try {
    // The CSRF token comes with GET /api/v1/me, so a direct visit works.
    const me = await apiClient.get<MeResponse>('/me')
    csrfToken.value = me.csrf_token
    isOwner.value = me.role === 'owner'

    await loadPeriods()
    if (!isOwner.value) {
      return
    }

    try {
      fill(await apiClient.get<ProfileResponse>('/profile'))
    } catch (error) {
      // 404: no profile saved yet, the form starts with its defaults.
      if (!(isApiError(error) && error.status === 404)) {
        throw error
      }
    }
  } catch (error) {
    if (isApiError(error) && error.isUnauthenticated) {
      await router.push({ name: 'login' })
      return
    }
    loadError.value = t('profile.loadError')
  } finally {
    loading.value = false
  }
})

async function save(): Promise<void> {
  saving.value = true
  saved.value = false
  saveError.value = ''
  errors.value = {}
  try {
    const response = await apiClient.put<ProfileResponse>('/profile', {
      legal_name: form.legal_name,
      street: form.street,
      address_line2: orNull(form.address_line2),
      postal_code: form.postal_code,
      city: form.city,
      country_code: form.country_code,
      tax_number: orNull(form.tax_number),
      vat_id: orNull(form.vat_id),
      kleinunternehmer_id: orNull(form.kleinunternehmer_id),
      default_vat_regime: form.default_vat_regime,
      default_language: form.default_language,
      bank_name: orNull(form.bank_name),
      iban: orNull(form.iban),
      bic: orNull(form.bic),
      email: orNull(form.email),
      phone: orNull(form.phone),
      website: orNull(form.website),
      footer_text: orNull(form.footer_text),
      number_formats: { invoice: form.invoice_format, offer: form.offer_format },
    })
    fill(response)
    saved.value = true
  } catch (error) {
    if (isApiError(error) && error.status === 400) {
      errors.value = error.details
    }
    saveError.value = t('profile.saveError')
  } finally {
    saving.value = false
  }
}

async function addPeriod(): Promise<void> {
  addingPeriod.value = true
  periodError.value = ''
  periodErrors.value = {}
  try {
    await apiClient.post('/vat-regimes', {
      valid_from: period.valid_from,
      regime: period.regime,
      note: orNull(period.note),
      supersedes_id: period.supersedes === 'none' ? null : period.supersedes,
    })
    period.valid_from = ''
    period.note = ''
    period.supersedes = 'none'
    await loadPeriods()
  } catch (error) {
    if (isApiError(error) && error.status === 400) {
      periodErrors.value = error.details
    }
    periodError.value =
      isApiError(error) && error.status === 409
        ? t('profile.vat.conflict')
        : t('profile.vat.addError')
  } finally {
    addingPeriod.value = false
  }
}
</script>

<template>
  <GovuePage :title="t('profile.title')" :lead="t('profile.lead')">
    <p v-if="loading">{{ t('profile.loading') }}</p>
    <p v-else-if="loadError" role="alert" class="text-danger-text">{{ loadError }}</p>

    <template v-else>
      <p v-if="!isOwner" role="status">{{ t('profile.ownerOnly') }}</p>

      <form v-if="isOwner" class="flex flex-col gap-6" novalidate @submit.prevent="save">
        <GovueCard :title="t('profile.sections.seller')">
          <div class="flex flex-col gap-4">
            <GovueField :label="t('profile.fields.legalName')" :error="errors.legal_name">
              <GovueInput v-model="form.legal_name" autocomplete="organization" />
            </GovueField>
            <GovueField :label="t('profile.fields.street')" :error="errors.street">
              <GovueInput v-model="form.street" autocomplete="address-line1" />
            </GovueField>
            <GovueField :label="t('profile.fields.addressLine2')" :error="errors.address_line2">
              <GovueInput v-model="form.address_line2" autocomplete="address-line2" />
            </GovueField>
            <GovueField :label="t('profile.fields.postalCode')" :error="errors.postal_code">
              <GovueInput v-model="form.postal_code" autocomplete="postal-code" />
            </GovueField>
            <GovueField :label="t('profile.fields.city')" :error="errors.city">
              <GovueInput v-model="form.city" autocomplete="address-level2" />
            </GovueField>
            <GovueField
              :label="t('profile.fields.countryCode')"
              :hint="t('profile.fields.countryCodeHint')"
              :error="errors.country_code"
            >
              <GovueInput v-model="form.country_code" autocomplete="country" />
            </GovueField>
          </div>
        </GovueCard>

        <GovueCard :title="t('profile.sections.tax')">
          <div class="flex flex-col gap-4">
            <GovueField :label="t('profile.fields.taxNumber')" :error="errors.tax_number">
              <GovueInput v-model="form.tax_number" />
            </GovueField>
            <GovueField :label="t('profile.fields.vatId')" :error="errors.vat_id">
              <GovueInput v-model="form.vat_id" />
            </GovueField>
            <GovueField
              :label="t('profile.fields.kleinunternehmerId')"
              :hint="t('profile.fields.kleinunternehmerHint')"
              :error="errors.kleinunternehmer_id"
            >
              <GovueInput v-model="form.kleinunternehmer_id" />
            </GovueField>
            <GovueField
              :label="t('profile.fields.defaultVatRegime')"
              :hint="t('profile.fields.defaultVatRegimeHint')"
              :error="errors.default_vat_regime"
            >
              <GovueSelect v-model="form.default_vat_regime" :options="regimeOptions" />
            </GovueField>
            <div class="flex flex-wrap items-center gap-2">
              <span class="text-sm font-medium text-text-muted">{{ t('profile.issueReady') }}</span>
              <GovueBadge
                v-for="regime in ['kleinunternehmer', 'standard']"
                :key="regime"
                :kind="issueReady[regime] ? 'success' : 'warning'"
              >
                {{ t(`profile.regime.${regime}`) }}:
                {{ issueReady[regime] ? t('profile.ready') : t('profile.incomplete') }}
              </GovueBadge>
            </div>
          </div>
        </GovueCard>

        <GovueCard :title="t('profile.sections.bank')">
          <div class="flex flex-col gap-4">
            <GovueField :label="t('profile.fields.bankName')" :error="errors.bank_name">
              <GovueInput v-model="form.bank_name" />
            </GovueField>
            <GovueField :label="t('profile.fields.iban')" :error="errors.iban">
              <GovueInput v-model="form.iban" autocomplete="off" />
            </GovueField>
            <GovueField :label="t('profile.fields.bic')" :error="errors.bic">
              <GovueInput v-model="form.bic" autocomplete="off" />
            </GovueField>
          </div>
        </GovueCard>

        <GovueCard :title="t('profile.sections.contact')">
          <div class="flex flex-col gap-4">
            <GovueField :label="t('profile.fields.email')" :error="errors.email">
              <GovueInput v-model="form.email" type="email" autocomplete="email" />
            </GovueField>
            <GovueField :label="t('profile.fields.phone')" :error="errors.phone">
              <GovueInput v-model="form.phone" type="tel" autocomplete="tel" />
            </GovueField>
            <GovueField :label="t('profile.fields.website')" :error="errors.website">
              <GovueInput v-model="form.website" type="url" autocomplete="url" />
            </GovueField>
            <GovueField :label="t('profile.fields.footerText')" :error="errors.footer_text">
              <GovueTextarea v-model="form.footer_text" :rows="3" />
            </GovueField>
            <GovueField
              :label="t('profile.fields.defaultLanguage')"
              :error="errors.default_language"
            >
              <GovueSelect v-model="form.default_language" :options="languageOptions" />
            </GovueField>
          </div>
        </GovueCard>

        <GovueCard :title="t('profile.sections.numbering')">
          <div class="flex flex-col gap-4">
            <p class="text-sm text-text-muted">{{ t('profile.fields.numberFormatHint') }}</p>
            <GovueField
              :label="t('profile.fields.invoiceFormat')"
              :error="errors['number_formats.invoice'] ?? errors.number_formats"
            >
              <GovueInput v-model="form.invoice_format" autocomplete="off" />
            </GovueField>
            <GovueField
              :label="t('profile.fields.offerFormat')"
              :error="errors['number_formats.offer']"
            >
              <GovueInput v-model="form.offer_format" autocomplete="off" />
            </GovueField>
          </div>
        </GovueCard>

        <div class="flex flex-col gap-2">
          <GovueButton
            type="submit"
            variant="primary"
            :loading="saving"
            :loading-label="t('profile.saving')"
          >
            {{ t('profile.save') }}
          </GovueButton>
          <p v-if="saved" role="status">{{ t('profile.saved') }}</p>
          <p v-if="saveError" role="alert" class="text-danger-text">{{ saveError }}</p>
        </div>
      </form>

      <GovueCard :title="t('profile.vat.title')">
        <div class="flex flex-col gap-4">
          <p class="text-sm text-text-muted">{{ t('profile.vat.lead') }}</p>

          <p v-if="periods.length === 0">{{ t('profile.vat.empty') }}</p>
          <ul v-else class="flex flex-col gap-1">
            <li v-for="p in periods" :key="p.id" :class="p.superseded ? 'line-through' : ''">
              {{ p.valid_from }}: {{ t(`profile.regime.${p.regime}`) }}
              <span v-if="p.note"> ({{ p.note }})</span>
              <span v-if="p.superseded" class="no-underline">
                {{ t('profile.vat.superseded') }}</span
              >
            </li>
          </ul>

          <form v-if="isOwner" class="flex flex-col gap-4" novalidate @submit.prevent="addPeriod">
            <GovueField
              :label="t('profile.vat.validFrom')"
              :hint="t('profile.vat.validFromHint')"
              :error="periodErrors.valid_from"
            >
              <GovueInput v-model="period.valid_from" autocomplete="off" />
            </GovueField>
            <GovueField :label="t('profile.vat.regimeLabel')" :error="periodErrors.regime">
              <GovueSelect v-model="period.regime" :options="regimeOptions" />
            </GovueField>
            <GovueField :label="t('profile.vat.note')" :error="periodErrors.note">
              <GovueInput v-model="period.note" />
            </GovueField>
            <GovueField
              :label="t('profile.vat.supersedes')"
              :hint="t('profile.vat.supersedesHint')"
              :error="periodErrors.supersedes_id"
            >
              <GovueSelect v-model="period.supersedes" :options="supersedeOptions" />
            </GovueField>
            <div class="flex flex-col gap-2">
              <GovueButton
                type="submit"
                variant="secondary"
                :loading="addingPeriod"
                :loading-label="t('profile.vat.adding')"
              >
                {{ t('profile.vat.add') }}
              </GovueButton>
              <p v-if="periodError" role="alert" class="text-danger-text">{{ periodError }}</p>
            </div>
          </form>
        </div>
      </GovueCard>
    </template>
  </GovuePage>
</template>
