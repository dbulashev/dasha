<script setup lang="ts">
import BloatSection from '@/components/indexes/BloatSection.vue'
import BtreeOnArraySection from '@/components/indexes/BtreeOnArraySection.vue'
import InvalidSection from '@/components/indexes/InvalidSection.vue'
import MissingSection from '@/components/indexes/MissingSection.vue'
import Similar1Section from '@/components/indexes/Similar1Section.vue'
import Similar2Section from '@/components/indexes/Similar2Section.vue'
import Similar3Section from '@/components/indexes/Similar3Section.vue'
import { useViewError } from '@/composables/useViewError'

const { errorMessage, onError } = useViewError()
</script>

<template>
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>
    {{ errorMessage }}
  </v-alert>

  <BloatSection @error="onError" />
  <InvalidSection @error="onError" />
  <MissingSection @error="onError" />
  <BtreeOnArraySection @error="onError" />
  <Similar1Section @error="onError" />
  <Similar2Section @error="onError" />
  <Similar3Section @error="onError" />
</template>
