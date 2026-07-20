<script setup lang="ts">
import HotObjectsSection from '@/components/hot/HotObjectsSection.vue'
import TopKBySizeSection from '@/components/tables/TopKBySizeSection.vue'
import CachingSection from '@/components/tables/CachingSection.vue'
import PartitionsSection from '@/components/tables/PartitionsSection.vue'
</script>

<template>
  <HotObjectsSection kind="table" />
  <TopKBySizeSection />
  <CachingSection />
  <PartitionsSection />
</template>
