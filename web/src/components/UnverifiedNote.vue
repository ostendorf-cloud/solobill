<script setup lang="ts">
// UnverifiedNote says that the reading of the low value asset and pool
// thresholds for a small business owner is not verified. Every asset view
// shows it, because the thresholds decide the methods on offer.
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>

<template>
  <p
    role="note"
    data-testid="unverified-note"
    class="rounded-lg border border-border bg-warning-subtle px-4 py-3 text-base text-warning-text"
  >
    {{ t('assets.unverifiedNote') }}
  </p>
</template>
