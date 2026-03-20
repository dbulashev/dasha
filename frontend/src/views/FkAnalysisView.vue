<script setup lang="ts">
import InvalidConstraintsSection from '@/components/fk/InvalidConstraintsSection.vue'
import TypeMismatchSection from '@/components/fk/TypeMismatchSection.vue'
import PossibleNullsSection from '@/components/fk/PossibleNullsSection.vue'
import PossibleSimilarSection from '@/components/fk/PossibleSimilarSection.vue'
import { useViewError } from '@/composables/useViewError'

const { errorMessage, onError } = useViewError()
</script>

<template>
  <v-alert v-if="errorMessage" type="error" class="mb-4" closable>{{ errorMessage }}</v-alert>
  <InvalidConstraintsSection @error="onError" />
  <TypeMismatchSection @error="onError" />
  <PossibleNullsSection @error="onError" />
  <PossibleSimilarSection @error="onError" />
</template>
