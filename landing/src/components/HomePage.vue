<script setup lang="ts">
import { GovueFooter, GovuePage, GovueSkipLink, useLanguage } from '@ostendorf-cloud/govue'
import type { LocaleCode } from '@ostendorf-cloud/govue'
import { computed } from 'vue'

import { LEGAL } from '../site'
import { texts } from '../texts'
import SiteHeader from './SiteHeader.vue'

// The start page. The marketing content is a later issue (#156): this page
// proves the pipeline, from the route table through the prerender to the
// server.
const { locale } = useLanguage()
const t = computed(() => texts[locale.value as LocaleCode].home)
</script>

<template>
  <GovueSkipLink />
  <SiteHeader path="/" />
  <GovuePage :title="t.title" :lead="t.lead">
    <p class="text-base text-text-muted">{{ t.note }}</p>
  </GovuePage>
  <GovueFooter :legal="LEGAL" />
</template>
