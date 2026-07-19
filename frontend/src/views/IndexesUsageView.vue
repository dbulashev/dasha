<script setup lang="ts">
import HotObjectsSection from '@/components/hot/HotObjectsSection.vue'
import UsageSection from '@/components/indexes/UsageSection.vue'
import UnusedSection from '@/components/indexes/UnusedSection.vue'
</script>

<template>

  <HotObjectsSection kind="index" />
  <UnusedSection />
  <UsageSection />
</template>
