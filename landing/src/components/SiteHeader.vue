<script setup lang="ts">
import {
  GovueHeader,
  GovueLanguageSwitch,
  GovueNav,
  GovueThemeSwitch,
  localizedPath,
  useLanguage,
  type LocaleCode,
  type NavItem,
} from '@ostendorf-cloud/govue'
import { computed } from 'vue'

import { APP_URL } from '../site'
import { texts } from '../texts'

// The header of every page: the brand, the navigation with the way into the
// app, the theme switch and the language switch. Each language is a real link
// to the same page in that language, so it works without script and a
// crawler follows it. The narrow setup below 1024px keeps the row one row on
// a phone: the cycle theme button and only the link to the other language.
const props = defineProps<{
  /** The route path of the page without a language prefix, such as /imprint. */
  path: string
}>()

const { locale } = useLanguage()

const items = computed<NavItem[]>(() => {
  const t = texts[locale.value as LocaleCode].nav
  return [
    {
      href: localizedPath('/', locale.value as LocaleCode),
      label: t.home,
      current: props.path === '/',
    },
    { href: APP_URL, label: t.login, variant: 'cta' },
  ]
})

function linkFor(code: string): string {
  return localizedPath(props.path, code as LocaleCode)
}
</script>

<template>
  <GovueHeader nav-placement="end">
    <template #brand>
      <a :href="localizedPath('/', locale as LocaleCode)" class="text-text">
        {{ texts[locale as LocaleCode].brand }}
      </a>
    </template>
    <GovueNav :items="items" />
    <template #theme>
      <span class="contents lg:hidden"><GovueThemeSwitch cycle /></span>
      <span class="hidden lg:contents"><GovueThemeSwitch compact /></span>
    </template>
    <template #actions>
      <span class="contents lg:hidden">
        <GovueLanguageSwitch :link-for="linkFor" others-only />
      </span>
      <span class="hidden lg:contents"><GovueLanguageSwitch :link-for="linkFor" /></span>
    </template>
  </GovueHeader>
</template>
