<script setup lang="ts">
import { ref } from 'vue'
import TopKBySizeSection from '@/components/indexes/TopKBySizeSection.vue'
import BloatSection from '@/components/indexes/BloatSection.vue'
import BtreeOnArraySection from '@/components/indexes/BtreeOnArraySection.vue'
import CachingSection from '@/components/indexes/CachingSection.vue'
import InvalidSection from '@/components/indexes/InvalidSection.vue'
import MissingSection from '@/components/indexes/MissingSection.vue'
import SimilarSection from '@/components/indexes/SimilarSection.vue'
import UnusedSection from '@/components/indexes/UnusedSection.vue'
import UsageSection from '@/components/indexes/UsageSection.vue'

const errorMessage = ref('')

function onError(msg: string) {
  errorMessage.value = msg
}
</script>

<template>
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>
    {{ errorMessage }}
  </v-alert>

  <TopKBySizeSection @error="onError" />
  <BloatSection @error="onError" />
  <BtreeOnArraySection @error="onError" />
  <CachingSection @error="onError" />
  <InvalidSection @error="onError" />
  <MissingSection @error="onError" />
  <SimilarSection @error="onError" />
  <UnusedSection @error="onError" />
  <UsageSection @error="onError" />
</template>
