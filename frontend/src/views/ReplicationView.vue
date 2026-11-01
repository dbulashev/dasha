<script setup lang="ts">
import ClusterStandbyLink from '@/components/ClusterStandbyLink.vue'
import ReplicationConfigSection from '@/components/replication/ReplicationConfigSection.vue'
import ReplicationStatusSection from '@/components/replication/ReplicationStatusSection.vue'
import ReplicationSlotsSection from '@/components/replication/ReplicationSlotsSection.vue'
</script>

<template>
  <ClusterStandbyLink class="mb-4" />
  <ReplicationConfigSection />
  <ReplicationStatusSection />
  <ReplicationSlotsSection />
</template>
