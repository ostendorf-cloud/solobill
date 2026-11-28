<script setup lang="ts">
import { GovueFooter, GovuePage, GovueSkipLink, useLanguage } from '@ostendorf-cloud/govue'
import type { LocaleCode } from '@ostendorf-cloud/govue'
import { computed } from 'vue'

import { siteConfig } from '../siteConfig'
import { LEGAL } from '../site'
import { texts } from '../texts'
import SiteHeader from './SiteHeader.vue'

// The start page. The marketing content is a later issue (#156): this page
// proves the pipeline, from the route table through the prerender to the
// server. The hero's two buttons lead into the app, to the URLs of the
// runtime configuration, see siteConfig.ts.
const { locale } = useLanguage()
const t = computed(() => texts[locale.value as LocaleCode].home)

// The same classes as GovueButton's primary and secondary variant, on a link:
// the buttons navigate, they submit nothing.
const buttonBase =
  'inline-flex min-h-11 items-center justify-center rounded-md px-4 text-base font-medium transition-colors'
const primary = `${buttonBase} bg-accent-strong text-accent-on hover:bg-accent-strong-hover`
const secondary = `${buttonBase} border border-border-strong bg-surface text-text hover:bg-surface-hover`
</script>

<template>
  <GovueSkipLink />
  <SiteHeader path="/" />
  <GovuePage :title="t.title" :lead="t.lead">
    <div class="flex flex-wrap gap-3" data-testid="hero-actions">
      <a :href="siteConfig.loginUrl" :class="primary">{{ t.login }}</a>
      <!-- Until the configuration is loaded, which is the prerendered markup,
           the register button keeps its place but is neither seen nor
           reachable, so a site with registration off never shows it. -->
      <a
        v-if="siteConfig.registrationEnabled"
        :href="siteConfig.registerUrl"
        :class="[secondary, siteConfig.loaded ? '' : 'invisible']"
        :aria-hidden="siteConfig.loaded ? undefined : 'true'"
        :tabindex="siteConfig.loaded ? undefined : -1"
        >{{ t.register }}</a
      >
    </div>
    <p class="text-base text-text-muted">{{ t.note }}</p>
  </GovuePage>
  <GovueFooter :legal="LEGAL" />
</template>
