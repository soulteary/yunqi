<template>
  <VMenu ref="menu" :model-value="modelValue" @update:model-value="emit('update:modelValue', $event)">
    <template #activator="slotProps">
      <slot name="activator" v-bind="slotProps"></slot>
    </template>
    <slot></slot>
  </VMenu>
</template>

<script setup lang="ts">
import { ref, toRef } from 'vue'
import { VMenu } from 'vuetify/components/VMenu'
import { useAnchoredOverlayPosition, type AnchoredOverlay } from '../composables/useAnchoredOverlayPosition'

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
const menu = ref<AnchoredOverlay | null>(null)
useAnchoredOverlayPosition(toRef(props, 'modelValue'), menu)
</script>
