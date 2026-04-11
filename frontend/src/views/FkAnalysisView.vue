<script setup lang="ts">
import InvalidConstraintsSection from '@/components/fk/InvalidConstraintsSection.vue'
import TypeMismatchSection from '@/components/fk/TypeMismatchSection.vue'
import PossibleNullsSection from '@/components/fk/PossibleNullsSection.vue'
import PossibleSimilarSection from '@/components/fk/PossibleSimilarSection.vue'
</script>

<template>
  <InvalidConstraintsSection />
  <TypeMismatchSection />
  <PossibleNullsSection />
  <PossibleSimilarSection />
</template>
