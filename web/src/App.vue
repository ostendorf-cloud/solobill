<script setup lang="ts">
import { GovueSkipLink } from '@ostendorf-cloud/govue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>

<template>
  <GovueSkipLink>{{ t('common.skipToContent') }}</GovueSkipLink>
  <RouterView />
</template>
