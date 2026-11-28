<script setup lang="ts">
import {
  GovueFooter,
  GovuePage,
  GovueSkipLink,
  localizedPath,
  useLanguage,
} from '@ostendorf-cloud/govue'
import type { LocaleCode } from '@ostendorf-cloud/govue'
import { computed } from 'vue'

import { LEGAL } from '../site'
import { texts } from '../texts'
import SiteHeader from './SiteHeader.vue'

// The 404 page of one language. The frontend server answers every unknown
// path with it, status 404, so its links are absolute paths of the language.
const { locale } = useLanguage()
const t = computed(() => texts[locale.value as LocaleCode].notFound)
</script>

<template>
  <GovueSkipLink />
  <SiteHeader path="/" />
  <GovuePage :title="t.title" :lead="t.lead">
    <p class="text-base text-text">
      <a :href="localizedPath('/', locale as LocaleCode)" class="text-accent-text underline">
        {{ t.back }}
      </a>
    </p>
  </GovuePage>
  <GovueFooter :legal="LEGAL" />
</template>
