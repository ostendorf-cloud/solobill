<script setup lang="ts">
// NotFoundView answers a path that is no page: an unknown language prefix or
// an unknown path below a known one.
import { GovuePage } from '@ostendorf-cloud/govue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'

const { t } = useI18n()
</script>

<template>
  <GovuePage :title="t('notFound.title')" :lead="t('notFound.lead')">
    <RouterLink :to="{ name: 'home' }" class="text-accent-text underline">
      {{ t('notFound.home') }}
    </RouterLink>
  </GovuePage>
</template>
