<script setup lang="ts">
// AppNav renders the main navigation from the routes' own `meta.nav`
// (navigationEntries, see router/meta.ts). A feature adds its entry in its
// route module; this component is not edited for it. An entry with `roles`
// stays hidden for any other role, and for an unknown role.
import { GovueNav, type NavItem } from '@ostendorf-cloud/govue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import { routeLocale } from '../router/locale'
import { navigationEntries } from '../router/meta'

const props = defineProps<{
  /** The signed in role, or undefined while it is not known. */
  role?: string
}>()

const { t } = useI18n()
const router = useRouter()
const route = useRoute()

const items = computed<NavItem[]>(() =>
  navigationEntries(router.options.routes.flatMap((r) => r.children ?? [r]))
    .filter((entry) => {
      const roles = entry.nav.roles
      return (
        !roles || (props.role !== undefined && (roles as readonly string[]).includes(props.role))
      )
    })
    .map((entry) => ({
      href: router.resolve({ name: entry.name, params: { locale: routeLocale(route) } }).href,
      label: t(entry.nav.labelKey),
      current: route.name === entry.name,
    })),
)

/** govue's nav renders plain links. A plain click is handled by the router
 *  so the app does not reload; a modified click keeps its browser meaning. */
function onClick(event: MouseEvent): void {
  if (event.defaultPrevented || event.button !== 0) {
    return
  }
  if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
    return
  }
  const link = (event.target as HTMLElement).closest('a')
  const href = link?.getAttribute('href')
  if (!href || !href.startsWith('/')) {
    return
  }
  event.preventDefault()
  void router.push(href)
}
</script>

<template>
  <div @click="onClick">
    <GovueNav
      :items="items"
      :aria-label="t('common.mainNavigation')"
      :toggle-label="t('common.menu')"
    />
  </div>
</template>
