<script setup lang="ts">
import { GovueFooter, GovueLegalPage, useLanguage } from '@ostendorf-cloud/govue'
import type { LegalPage, LocaleCode } from '@ostendorf-cloud/govue'
import { computed } from 'vue'

import { LEGAL } from '../site'
import { texts } from '../texts'

// A legal page of the site. The text is a placeholder that says so: the
// operator writes the real text for this site's features and reviews it
// before it goes live. govue ships none and nothing here is legal advice.
const props = defineProps<{ page: LegalPage }>()

const { locale } = useLanguage()
const t = computed(() => texts[locale.value as LocaleCode].legalPlaceholder)
</script>

<template>
  <GovueLegalPage :page="props.page">
    <section data-placeholder="legal">
      <h2 class="text-xl font-semibold text-text">{{ t.heading }}</h2>
      <p class="text-base text-text">{{ t.text }}</p>
    </section>
  </GovueLegalPage>
  <GovueFooter :legal="LEGAL" />
</template>
