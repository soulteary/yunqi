<template>
  <v-card variant="outlined" class="mb-4">
    <v-card-title>{{ t('priceReview.title') }}</v-card-title>
    <v-card-text>
      <p class="text-caption mb-3">{{ t('priceReview.scope') }}</p>
      <v-btn size="small" :loading="busy" @click="run(() => api.getPriceReview())">{{ t('priceReview.load') }}</v-btn>
      <v-btn v-if="state" size="small" class="ml-2" :disabled="busy" @click="run(() => api.checkPresetPrices())">{{ t('priceReview.check') }}</v-btn>
      <v-alert v-if="error" type="error" variant="tonal" class="mt-3">{{ error }}</v-alert>
      <template v-if="state">
        <v-alert v-if="state.lastCheckError" type="warning" variant="tonal" class="mt-3">{{ t('priceReview.checkFailed') }}</v-alert>
        <p class="text-caption mt-3">{{ t('priceReview.lastCheck') }}: {{ state.lastCheckAt || t('priceReview.unknown') }}</p>
        <p class="text-caption">{{ t(state.signatureRequired ? 'priceReview.signatureRequired' : 'priceReview.signatureOptional') }}</p>
        <p class="text-caption">{{ state.sourceMaxAge === '0s' && state.sourceFutureSkew === '0s' ? t('priceReview.freshnessDisabled') : t('priceReview.freshnessPolicy', { age: state.sourceMaxAge === '0s' ? t('priceReview.disabled') : state.sourceMaxAge, skew: state.sourceFutureSkew }) }}</p>
        <p class="text-caption mt-3">{{ t('priceReview.active') }}: {{ state.activeDataVersion }} · {{ state.activeVersion }}</p>
        <p v-if="state.activePriceSource" class="text-caption">{{ t(state.activePriceSource.signatureVerified ? 'priceReview.verified' : 'priceReview.unverified') }} · {{ state.activePriceSource.dataVersion }} · {{ state.activePriceSource.sourceDigest }}</p>
        <p v-if="state.activePriceSource" class="text-caption">{{ t('priceReview.publishedAt') }}: {{ state.activePriceSource.sourcePublishedAt || t('priceReview.unknown') }} · {{ t('priceReview.checkedAt') }}: {{ state.activePriceSource.sourceCheckedAt || t('priceReview.unknown') }}</p>
        <v-alert v-if="state.rollbackBlockedThrough" type="info" variant="tonal" class="my-3">{{ t('priceReview.rollbackCheckpoint', { version: state.rollbackBlockedThrough }) }}</v-alert>
        <template v-if="state.pending">
          <v-alert v-if="state.pending.sourceKind === 'legacy_cache'" type="info" variant="tonal" class="my-3">{{ t('priceReview.legacyCache') }}</v-alert>
          <v-alert v-if="state.pending.policyError" type="error" variant="tonal" class="my-3">{{ state.pending.policyError }}</v-alert>
          <v-alert type="warning" variant="tonal" class="my-3">{{ t('priceReview.pending', { count: state.pending.changeCount, threshold: state.pending.thresholdPercent }) }}</v-alert>
          <p class="text-caption">{{ t('priceReview.publishedAt') }}: {{ state.pending.sourcePublishedAt || t('priceReview.unknown') }} · {{ t('priceReview.checkedAt') }}: {{ state.pending.sourceCheckedAt }}</p>
          <p>{{ state.pending.dataVersion }} · {{ state.pending.sourceUrl }}</p>
          <p class="text-caption">SHA256: {{ state.pending.sourceDigest }}</p>
          <p :class="state.pending.signatureVerified ? 'text-success' : 'text-warning'">{{ t(state.pending.signatureVerified ? 'priceReview.verified' : 'priceReview.unverified') }} {{ state.pending.signingKeyId || '' }}</p>
          <p v-if="state.pending.previewTruncated" class="text-warning">{{ t('priceReview.truncated') }}</p>
          <v-table density="compact" class="my-3">
            <thead><tr><th>{{ t('priceReview.model') }}</th><th>{{ t('priceReview.before') }}</th><th>{{ t('priceReview.after') }}</th></tr></thead>
            <tbody><tr v-for="change in visibleChanges" :key="change.model">
              <td :class="change.anomaly ? 'text-warning' : ''">{{ change.model }}<div v-for="reason in change.reasons" :key="reason" class="text-caption">{{ t(`priceReview.reason.${reason}`) }}</div></td>
              <td><pre class="price-json">{{ change.before ? JSON.stringify(change.before, null, 2) : t('priceReview.noPrice') }}</pre></td>
              <td><pre class="price-json">{{ change.after ? JSON.stringify(change.after, null, 2) : t('priceReview.noPrice') }}</pre></td>
            </tr></tbody>
          </v-table>
          <v-pagination v-if="state.pending.changes.length > 10" v-model="page" :length="Math.ceil(state.pending.changes.length / 10)" density="compact" />
          <v-checkbox v-if="state.pending.requiresAnomalyAcknowledgement" v-model="allowAnomalies" :label="t('priceReview.acknowledge')" hide-details />
          <v-btn class="mt-3" color="warning" :disabled="busy || !!state.pending.policyError || (state.pending.requiresAnomalyAcknowledgement && !allowAnomalies)" @click="approve">{{ t('priceReview.approve') }}</v-btn>
        </template>
        <p v-else class="mt-3">{{ t('priceReview.noPending') }}</p>
        <template v-if="rollbackOptions.length">
          <p class="text-warning text-caption mt-4">{{ t('priceReview.rollbackScope') }}</p>
          <v-select v-model="rollbackHash" :items="rollbackOptions" :label="t('priceReview.history')" density="compact" />
          <v-btn size="small" :disabled="busy || !rollbackHash" @click="run(() => api.rollbackPresetPrices(rollbackHash))">{{ t('priceReview.rollback') }}</v-btn>
        </template>
        <p v-for="entry in auditPreview" :key="`${entry.at}-${entry.action}`" class="text-caption mt-2">{{ entry.at }} · {{ entry.actorId }} · {{ entry.action }} · {{ entry.versionId }}</p>
      </template>
    </v-card-text>
  </v-card>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from '../i18n'
import { api, ApiError } from '../services/api'
import type { PriceReviewResponse, PriceReviewState } from '../services/types/price-review'
const { t } = useI18n()
const state = ref<PriceReviewState | null>(null)
const busy = ref(false)
const error = ref('')
const allowAnomalies = ref(false)
const rollbackHash = ref('')
const page = ref(1)
const visibleChanges = computed(() => state.value?.pending?.changes.slice((page.value - 1) * 10, page.value * 10) || [])
const rollbackOptions = computed(() => state.value?.history.filter(item => item.bundleHash !== state.value?.activeBundleHash).map(item => ({ title: `${item.dataVersion} · ${item.bundleHash.slice(0, 12)}`, value: item.bundleHash })) || [])
const auditPreview = computed(() => state.value?.audit.slice(-5).reverse() || [])
function applyState(value: PriceReviewState) { state.value = value; allowAnomalies.value = false; rollbackHash.value = ''; page.value = 1 }
async function run(action: () => Promise<PriceReviewResponse>) {
  if (busy.value) return
  busy.value = true; error.value = ''
  try { const result = await action(); applyState(result.state) }
  catch (e) {
    if (e instanceof ApiError && e.details && typeof e.details === 'object' && 'state' in e.details) applyState((e.details as { state: PriceReviewState }).state)
    error.value = e instanceof Error ? e.message : String(e)
  } finally { busy.value = false }
}
async function approve() {
  if (!state.value?.pending || state.value.pending.policyError || (state.value.pending.requiresAnomalyAcknowledgement && !allowAnomalies.value)) return
  const id = state.value.pending.reviewId
  const acknowledged = allowAnomalies.value
  await run(() => api.approvePresetPrices(id, acknowledged))
}
</script>
<style scoped>
.price-json { max-width: 28rem; max-height: 16rem; overflow: auto; white-space: pre-wrap; font-size: 0.75rem; }
</style>
