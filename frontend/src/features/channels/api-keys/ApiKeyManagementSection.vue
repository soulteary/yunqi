<template>
  <div class="api-key-management-section">
    <v-card variant="outlined" rounded="lg" :color="hasConfigurableKeys ? undefined : 'error'">
      <v-card-title class="d-flex align-center justify-space-between pa-4 pb-2">
        <div class="d-flex align-center ga-2">
          <v-icon :color="hasConfigurableKeys ? 'primary' : 'error'">mdi-key</v-icon>
          <span class="section-title">
            {{ t('channelCard.apiKeyManagement') }}<span v-if="serviceType !== 'copilot'"> *</span>
          </span>
          <v-chip v-if="!hasConfigurableKeys" size="x-small" color="error" variant="tonal">
            {{ t('channelEditor.auth.apiKeyRequired') }}
          </v-chip>
        </div>
        <v-chip size="small" color="info" variant="tonal">
          {{ t('addChannel.apiKeyLoadBalance') }}
        </v-chip>
      </v-card-title>

      <v-card-text class="pt-2">
        <v-alert
          v-if="restoringKey || restoringKeyModel"
          color="info"
          variant="tonal"
          density="compact"
          class="mb-3"
          role="status"
          aria-live="polite"
        >
          <div class="d-flex align-center ga-2">
            <v-progress-circular indeterminate size="16" width="2" />
            <span>{{ t('channelCard.restoreKeyInProgress') }}</span>
          </div>
        </v-alert>

        <div v-if="providerId === 'volcengine' && accountUid" class="d-flex align-center justify-space-between ga-3 flex-wrap mb-3">
          <div class="d-flex align-center ga-2">
            <v-icon color="primary" size="small">mdi-shield-key-outline</v-icon>
            <span class="text-body-2 font-weight-medium">{{ t('volcengineAccessKey.title') }}</span>
          </div>
          <v-btn
            size="small"
            variant="text"
            color="primary"
            href="https://console.volcengine.com/iam/keymanage/"
            target="_blank"
            rel="noopener noreferrer"
          >
            <v-icon start size="small">mdi-open-in-new</v-icon>
            {{ t('volcengineAccessKey.openConsole') }}
          </v-btn>
        </div>
        <v-progress-linear
          v-if="providerId === 'volcengine' && volcengineCredentialsLoading"
          indeterminate
          color="primary"
          class="mb-3"
        />
        <v-alert
          v-if="providerId === 'volcengine' && volcengineCredentialsError"
          color="error"
          variant="tonal"
          density="compact"
          class="mb-3"
        >
          {{ volcengineCredentialsError }}
        </v-alert>

        <div v-if="providerId === 'kimi' && accountUid" class="d-flex align-center justify-space-between ga-3 flex-wrap mb-2">
          <div class="d-flex align-center ga-2">
            <v-icon color="primary" size="small">mdi-chart-donut</v-icon>
            <span class="text-body-2 font-weight-medium">{{ t('kimiConsoleToken.title') }}</span>
          </div>
          <v-btn
            size="small"
            variant="text"
            color="primary"
            href="https://www.kimi.com/code/console"
            target="_blank"
            rel="noopener noreferrer"
          >
            <v-icon start size="small">mdi-open-in-new</v-icon>
            {{ t('kimiConsoleToken.openConsole') }}
          </v-btn>
        </div>
        <div v-if="providerId === 'kimi' && accountUid" class="text-caption text-medium-emphasis mb-3">
          {{ t('kimiConsoleToken.hint') }}
        </div>
        <v-progress-linear
          v-if="providerId === 'kimi' && kimiCredentialsLoading"
          indeterminate
          color="primary"
          class="mb-3"
        />
        <v-alert
          v-if="providerId === 'kimi' && kimiCredentialsError"
          color="error"
          variant="tonal"
          density="compact"
          class="mb-3"
        >
          {{ kimiCredentialsError }}
        </v-alert>

        <div v-if="providerId === 'mimo' && accountUid" class="d-flex align-center justify-space-between ga-3 flex-wrap mb-2">
          <div class="d-flex align-center ga-2">
            <v-icon color="primary" size="small">mdi-cookie-cog-outline</v-icon>
            <span class="text-body-2 font-weight-medium">{{ t('mimoConsoleCookie.title') }}</span>
          </div>
          <v-btn
            size="small"
            variant="text"
            color="primary"
            href="https://platform.xiaomimimo.com/console/plan-manage"
            target="_blank"
            rel="noopener noreferrer"
          >
            <v-icon start size="small">mdi-open-in-new</v-icon>
            {{ t('mimoConsoleCookie.openConsole') }}
          </v-btn>
        </div>
        <div v-if="providerId === 'mimo' && accountUid" class="text-caption text-medium-emphasis mb-3">
          {{ t('mimoConsoleCookie.hint') }}
        </div>
        <v-progress-linear
          v-if="providerId === 'mimo' && mimoCredentialsLoading"
          indeterminate
          color="primary"
          class="mb-3"
        />
        <v-alert
          v-if="providerId === 'mimo' && mimoCredentialsError"
          color="error"
          variant="tonal"
          density="compact"
          class="mb-3"
        >
          {{ mimoCredentialsError }}
        </v-alert>

        <div v-if="providerId === 'compshare' && accountUid" class="d-flex align-center justify-space-between ga-3 flex-wrap mb-2">
          <div class="d-flex align-center ga-2">
            <v-icon color="primary" size="small">mdi-gauge</v-icon>
            <span class="text-body-2 font-weight-medium">{{ t('compshareConsoleCookie.title') }}</span>
          </div>
          <v-btn
            size="small"
            variant="text"
            color="primary"
            href="https://console.compshare.cn/light-gpu/model-manage"
            target="_blank"
            rel="noopener noreferrer"
          >
            <v-icon start size="small">mdi-open-in-new</v-icon>
            {{ t('compshareConsoleCookie.openConsole') }}
          </v-btn>
        </div>
        <div v-if="providerId === 'compshare' && accountUid" class="text-caption text-medium-emphasis mb-3">
          {{ t('compshareConsoleCookie.hint') }}
        </div>
        <v-progress-linear
          v-if="providerId === 'compshare' && compshareCredentialsLoading"
          indeterminate
          color="primary"
          class="mb-3"
        />
        <v-alert
          v-if="providerId === 'compshare' && compshareCredentialsError"
          color="error"
          variant="tonal"
          density="compact"
          class="mb-3"
        >
          {{ compshareCredentialsError }}
        </v-alert>

        <v-progress-linear
          v-if="providerId === 'minimax' && minimaxEndpointsLoading"
          indeterminate
          color="primary"
          class="mb-3"
        />
        <v-alert
          v-if="providerId === 'minimax' && minimaxEndpointsError"
          color="error"
          variant="tonal"
          density="compact"
          class="mb-3"
        >
          {{ minimaxEndpointsError }}
        </v-alert>

        <!-- 现有密钥列表（拉黑状态与 provider 用量均归并到对应 Key） -->
        <div v-if="keyRows.length" class="mb-4">
          <v-list density="compact" class="bg-transparent">
            <draggable
              v-model="sortableKeyRows"
              item-key="key"
              handle=".key-drag-handle"
              ghost-class="key-ghost"
              draggable=".key-row-sortable"
              class="key-sortable-list"
              :disabled="!canReorderKeys"
            >
              <template #item="{ element: row }">
                <div
                  class="mb-2"
                  :class="{ 'key-row-sortable': !row.disabled && canReorderKeys }"
                  :data-key-row="row.key"
                >
              <v-list-item
                rounded="lg"
                variant="tonal"
                prepend-gap="8"
                role="button"
                :aria-expanded="expandedDetailKey === row.key"
                :color="row.disabled ? 'warning' : duplicateKeyIndex === row.activeIndex ? 'error' : 'surface-variant'"
                :class="{
                  'animate-pulse': duplicateKeyIndex === row.activeIndex,
                  'volcengine-key-row': !!(row.planCredential || row.minimaxEndpoint),
                }"
                class="cursor-pointer"
                @click="onKeyRowClick(row)"
                @keydown.enter.prevent="onKeyRowClick(row)"
              >
                <template #prepend>
                  <div class="d-flex align-center">
                    <v-icon
                      v-if="!row.disabled && canReorderKeys"
                      size="small"
                      color="grey"
                      class="key-drag-handle mr-2"
                    >
                      mdi-drag-vertical
                    </v-icon>
                    <v-icon
                      size="small"
                      :color="row.disabled ? 'warning' : duplicateKeyIndex === row.activeIndex ? 'error' : 'primary'"
                    >
                      {{ row.disabled ? 'mdi-key-alert' : duplicateKeyIndex === row.activeIndex ? 'mdi-alert' : 'mdi-key' }}
                    </v-icon>
                  </div>
                </template>

                <v-list-item-title>
                  <div class="d-flex align-center ga-2 flex-wrap">
                    <code class="text-caption">{{ maskApiKey(row.key) }}</code>
                    <v-chip
                      v-if="row.disabled"
                      size="x-small"
                      :color="disabledKeyColor(row.disabled.reason)"
                      variant="tonal"
                    >
                      {{ t(getDisabledKeyLabel(row.disabled.reason)) }}
                    </v-chip>
                    <v-chip
                      v-else-if="row.enabled === false"
                      size="x-small"
                      color="warning"
                      variant="tonal"
                    >
                      {{ t('channelCard.keyPaused') }}
                    </v-chip>
                    <span v-if="row.disabled?.recoverAt" class="text-caption text-medium-emphasis">
                      {{ t('channelCard.recoverAt') }}: {{ formatDisabledTime(row.disabled.recoverAt) }}
                    </span>
                    <div v-if="!row.disabled" class="d-flex align-center ga-1">
                    <!-- Models 状态标签 -->
                    <v-chip
                      v-if="keyModelsStatus.get(row.key)?.loading"
                      size="x-small"
                      color="info"
                      variant="tonal"
                    >
                      <v-icon start size="12">mdi-loading</v-icon>
                      {{ t('addChannel.checking') }}
                    </v-chip>
                    <v-tooltip
                      v-else-if="keyModelsStatus.get(row.key)?.success"
                      :text="t('addChannel.modelsCountTooltip')"
                      location="top"
                      max-width="300"
                      content-class="key-tooltip"
                    >
                      <template #activator="{ props: tooltipProps }">
                        <v-chip
                          v-bind="tooltipProps"
                          size="x-small"
                          color="success"
                          variant="tonal"
                        >
                          {{ t('addChannel.modelsCount', { statusCode: keyModelsStatus.get(row.key)?.statusCode ?? 'OK', count: keyModelsStatus.get(row.key)?.modelCount ?? 0 }) }}
                        </v-chip>
                      </template>
                    </v-tooltip>
                    <v-tooltip
                      v-else-if="keyModelsStatus.get(row.key)?.error"
                      :text="keyModelsStatus.get(row.key)?.error"
                      location="top"
                      max-width="300"
                      content-class="key-tooltip"
                    >
                      <template #activator="{ props: tooltipProps }">
                        <v-chip
                          v-bind="tooltipProps"
                          size="x-small"
                          color="error"
                          variant="tonal"
                        >
                          models {{ keyModelsStatus.get(row.key)?.statusCode || 'ERR' }}
                        </v-chip>
                      </template>
                    </v-tooltip>
                    <!-- 重复密钥标签 -->
                    <v-chip v-if="duplicateKeyIndex === row.activeIndex" size="x-small" color="error" variant="text">
                      {{ t('channelEditor.auth.duplicateKey') }}
                    </v-chip>
                  </div>
                  </div>
                </v-list-item-title>
                <v-list-item-subtitle v-if="row && (row.multiplierSource || row.groupMultiplier != null || row.maxGroupMultiplier != null || (row.keyUid && channelUid && channelKind))" class="mt-1">
                  <div class="d-flex align-center ga-1 flex-wrap text-caption">
                    <template v-if="row.multiplierSource || row.groupMultiplier != null || row.maxGroupMultiplier != null">
                    <v-chip size="x-small" variant="tonal" :color="multiplierStatusColor(row.multiplierSyncStatus)">
                      <template v-if="!row.multiplierSource || row.multiplierSource === 'manual'">
                        {{ multiplierStatusLabel('manual') }}
                      </template>
                      <template v-else>
                        {{ row.multiplierSource }} · {{ multiplierStatusLabel(row.multiplierSyncStatus || 'manual') }}
                      </template>
                    </v-chip>
                    <v-chip
                      v-if="row.consumptionPolicy === 'opportunistic'"
                      size="x-small"
                      color="warning"
                      variant="tonal"
                    >
                      {{ t('subscription.keyMultiplier.policyChip') }}
                    </v-chip>
                    <span>{{ t('subscription.keyMultiplier.group') }}: {{ row.quotaGroup || '-' }}</span>
                    <span>{{ t('subscription.keyMultiplier.value') }}: {{ row.groupMultiplier ?? '-' }}</span>
                    <span>{{ t('subscription.keyMultiplier.max') }}: {{ row.maxGroupMultiplier ?? props.channelMaxGroupMultiplier ?? '-' }}</span>
                    <span v-if="row.effectiveCostClass">{{ t('subscription.keyMultiplier.effectiveCostClass') }}: {{ row.effectiveCostClass }}</span>
                    <span v-if="row.multiplierExpiresAt">TTL: {{ formatDisabledTime(row.multiplierExpiresAt) }}</span>
                    <span :class="row.eligible === false ? 'text-error' : 'text-success'">
                      {{ row.eligible === false ? (row.ineligibleReason || t('subscription.keyMultiplier.ineligible')) : t('subscription.keyMultiplier.eligible') }}
                    </span>
                    </template>
                  </div>
                </v-list-item-subtitle>
                <v-list-item-subtitle v-if="row.volcengineCredential" class="mt-1 text-caption">
                  <v-icon size="12" class="mr-1">mdi-chart-timeline-variant</v-icon>
                  <template v-for="(part, idx) in volcengineUsageSummaryParts(row.volcengineCredential)" :key="part.labelKey || idx">
                    <span v-if="idx > 0" class="text-medium-emphasis"> · </span>
                    <span :class="part.colorClass || 'text-medium-emphasis'">{{ part.text }}</span>
                  </template>
                </v-list-item-subtitle>
                <v-list-item-subtitle v-if="row.kimiCredential" class="mt-1 text-caption">
                  <v-icon size="12" class="mr-1">mdi-chart-donut</v-icon>
                  <template v-for="(part, idx) in kimiUsageSummaryParts(row.kimiCredential)" :key="part.labelKey || idx">
                    <span v-if="idx > 0" class="text-medium-emphasis"> · </span>
                    <span :class="part.colorClass || 'text-medium-emphasis'">{{ part.text }}</span>
                  </template>
                </v-list-item-subtitle>
                <v-list-item-subtitle v-if="row.mimoCredential" class="mt-1 text-caption">
                  <v-icon size="12" class="mr-1">mdi-cookie-cog-outline</v-icon>
                  {{ mimoUsageSummary(row.mimoCredential) }}
                </v-list-item-subtitle>
                <v-list-item-subtitle v-if="row.compshareCredential" class="mt-1 text-caption">
                  <v-icon size="12" class="mr-1">mdi-gauge</v-icon>
                  {{ compshareUsageSummary(row.compshareCredential) }}
                </v-list-item-subtitle>
                <v-list-item-subtitle v-if="row.minimaxEndpoint" class="mt-1 text-caption">
                  <v-icon size="12" class="mr-1">mdi-lightning-bolt-outline</v-icon>
                  {{ minimaxUsageSummary(row.minimaxEndpoint) }}
                </v-list-item-subtitle>

                <template #append>
                  <div class="d-flex align-center ga-1" @click.stop>
                    <!-- 统一详情入口（渠道列表同款下箭头）：展开后同块承载 Key 倍率与分组模型排除 -->
                    <v-tooltip
                      v-if="!row.disabled && row.keyUid && channelUid && channelKind"
                      :text="t('channelCard.keyDetail')"
                      location="top"
                      :open-delay="150"
                      content-class="yunqi-tooltip"
                    >
                      <template #activator="{ props: tooltipProps }">
                        <v-btn
                          v-bind="tooltipProps"
                          :aria-label="t('channelCard.keyDetail')"
                          size="small"
                          :color="expandedDetailKey === row.key ? 'primary' : 'default'"
                          icon
                          variant="text"
                          @click="toggleKeyDetail(row)"
                        >
                          <v-icon size="small">{{ expandedDetailKey === row.key ? 'mdi-chevron-up' : 'mdi-chevron-down' }}</v-icon>
                        </v-btn>
                      </template>
                    </v-tooltip>
                    <v-btn
                      v-if="row.disabled"
                      size="small"
                      color="success"
                      variant="tonal"
                      rounded="lg"
                      :loading="restoringKey === row.key"
                      :disabled="!!restoringKey"
                      @click="$emit('restore-key', row.key)"
                    >
                      <v-icon start size="small">mdi-restore</v-icon>
                      {{ t('channelCard.restoreKey') }}
                    </v-btn>
                    <v-tooltip
                      v-else-if="row.enabled === false"
                      :text="t('channelCard.resumeKey')"
                      location="top"
                      :open-delay="150"
                      content-class="key-tooltip"
                    >
                      <template #activator="{ props: tooltipProps }">
                        <v-btn
                          v-bind="tooltipProps"
                          size="small"
                          color="success"
                          variant="tonal"
                          rounded="lg"
                          icon
                          :loading="suspendingKey === row.key"
                          :disabled="!!suspendingKey"
                          @click="$emit('resume-key', row.key)"
                        >
                          <v-icon size="small">mdi-play</v-icon>
                        </v-btn>
                      </template>
                    </v-tooltip>
                    <v-tooltip
                      v-else
                      :text="t('channelCard.suspendKey')"
                      location="top"
                      :open-delay="150"
                      content-class="key-tooltip"
                    >
                      <template #activator="{ props: tooltipProps }">
                        <v-btn
                          v-bind="tooltipProps"
                          size="small"
                          color="warning"
                          variant="tonal"
                          rounded="lg"
                          icon
                          :loading="suspendingKey === row.key"
                          :disabled="!!suspendingKey"
                          @click="$emit('suspend-key', row.key)"
                        >
                          <v-icon size="small">mdi-pause</v-icon>
                        </v-btn>
                      </template>
                    </v-tooltip>
                    <v-btn
                      v-if="row.planCredential || row.minimaxEndpoint"
                      size="small"
                      color="primary"
                      icon
                      variant="text"
                      :aria-label="planRowTitle()"
                      @click="toggleCredentialKey(row.key)"
                    >
                      <v-icon size="small">
                        {{ expandedCredentialKey === row.key ? 'mdi-chevron-up' : 'mdi-chevron-down' }}
                      </v-icon>
                    </v-btn>
                    <template v-if="!row.disabled && canReorderKeys">
                  <!-- 置顶/置底：每个活跃 Key 均可使用 -->
                  <v-tooltip
                    :disabled="isFirstActiveKey(row)"
                    :text="t('channelCard.moveTop')"
                    location="top"
                    :open-delay="150"
                    content-class="key-tooltip"
                  >
                    <template #activator="{ props: tooltipProps }">
                      <v-btn
                        v-bind="tooltipProps"
                        size="small"
                        color="warning"
                        icon
                        variant="text"
                        rounded="md"
                        :disabled="isFirstActiveKey(row)"
                        @click="moveKeyToTop(row)"
                      >
                        <v-icon size="small">mdi-arrow-collapse-up</v-icon>
                      </v-btn>
                    </template>
                  </v-tooltip>
                  <v-tooltip
                    :disabled="isLastActiveKey(row)"
                    :text="t('channelCard.moveBottom')"
                    location="top"
                    :open-delay="150"
                    content-class="key-tooltip"
                  >
                    <template #activator="{ props: tooltipProps }">
                      <v-btn
                        v-bind="tooltipProps"
                        size="small"
                        color="warning"
                        icon
                        variant="text"
                        rounded="md"
                        :disabled="isLastActiveKey(row)"
                        @click="moveKeyToBottom(row)"
                      >
                        <v-icon size="small">mdi-arrow-collapse-down</v-icon>
                      </v-btn>
                    </template>
                  </v-tooltip>
                    </template>
                  <v-tooltip
                    :text="copiedKey === row.key ? t('channelCard.copied') : t('channelCard.copyKey')"
                    location="top"
                    :open-delay="150"
                    content-class="key-tooltip"
                  >
                    <template #activator="{ props: tooltipProps }">
                      <v-btn
                        v-bind="tooltipProps"
                        size="small"
                        :color="copiedKey === row.key ? 'success' : 'primary'"
                        icon
                        variant="text"
                        @click="copyKey(row.key)"
                      >
                        <v-icon size="small">{{
                          copiedKey === row.key ? 'mdi-check' : 'mdi-content-copy'
                        }}</v-icon>
                      </v-btn>
                    </template>
                  </v-tooltip>
                  <v-tooltip :text="t('addChannel.deleteKey')" location="top" :open-delay="150" content-class="key-tooltip">
                    <template #activator="{ props: tooltipProps }">
                      <v-btn
                        v-bind="tooltipProps"
                        size="small"
                        color="error"
                        icon
                        variant="text"
                        :loading="removingKey === row.key"
                        :disabled="!!removingKey"
                        @click="handleRemoveKey(row)"
                      >
                        <v-icon size="small" color="error">mdi-close</v-icon>
                      </v-btn>
                    </template>
                  </v-tooltip>
                  </div>
                </template>
              </v-list-item>

              <v-expand-transition>
                <!-- Key 行统一详情面板：倍率（变更即保存）与分组模型排除（模型定稿即提交）同块展示 -->
                <div v-if="row.keyUid && channelUid && channelKind && expandedDetailKey === row.key" class="volcengine-key-detail px-4 pt-3 pb-4">
                  <div class="text-caption text-medium-emphasis mb-2">
                    <code>{{ maskApiKey(row.key) }}</code>
                    <v-chip size="x-small" color="secondary" variant="tonal" class="ml-2">
                      {{ editingQuotaGroup || t('channelCard.independentKey') }}
                    </v-chip>
                    <span class="ml-2">{{ editingQuotaGroup ? t('channelCard.affectedGroupKeys', { count: groupModelAffectedCount }) : t('channelCard.affectedCurrentKey') }}</span>
                  </div>
                  <v-row dense>
                    <v-col cols="12" sm="6">
                      <v-select
                        v-model="multiplierForm.consumptionPolicy"
                        :items="consumptionPolicyOptions"
                        item-title="title"
                        item-value="value"
                        :label="t('subscription.keyMultiplier.policy')"
                        clearable
                        variant="outlined"
                        density="compact"
                        hide-details
                        @update:model-value="applyMultiplierToConfigs"
                      />
                    </v-col>
                    <v-col cols="12" sm="6">
                      <v-text-field
                        v-model="multiplierForm.groupMultiplier"
                        type="number"
                        min="0"
                        step="any"
                        :disabled="multiplierEditing?.multiplierSource === 'new_api'"
                        :label="t('subscription.keyMultiplier.value')"
                        variant="outlined"
                        density="compact"
                        hide-details
                        @update:model-value="applyMultiplierToConfigs"
                        @keydown.enter="flushMultiplierInput"
                      />
                    </v-col>
                  </v-row>
                  <!-- 两条 caption 与上方两列输入框对齐：左=暂存说明，右=渠道上限（原输入框 hint，与暂存行错行） -->
                  <v-row dense class="mt-1">
                    <v-col cols="12" sm="6">
                      <div class="d-flex align-center ga-2 text-caption text-medium-emphasis">
                        <v-icon size="14">mdi-content-save-edit-outline</v-icon>
                        <span>{{ t('subscription.keyMultiplier.stagedHint') }}</span>
                      </div>
                    </v-col>
                    <v-col v-if="channelMaxGroupMultiplierHint" cols="12" sm="6">
                      <div class="text-caption text-medium-emphasis">{{ channelMaxGroupMultiplierHint }}</div>
                    </v-col>
                  </v-row>
                  <v-alert
                    v-if="multiplierForm.consumptionPolicy === 'opportunistic'"
                    color="warning"
                    variant="tonal"
                    density="compact"
                    class="mt-3"
                  >
                    {{ t('subscription.keyMultiplier.policyHint') }}
                  </v-alert>

                  <v-divider class="my-3" />

                  <div class="text-subtitle-2 font-weight-medium mb-1">{{ t(editingQuotaGroup ? 'channelCard.groupModelPolicy' : 'channelCard.keyModelPolicy') }}</div>
                  <v-combobox
                    v-model="groupModelForm.model"
                    :items="modelOptions"
                    item-title="title"
                    item-value="value"
                    :return-object="false"
                    auto-select-first
                    :label="t('channelCard.groupModelModel')"
                    :placeholder="t('channelCard.groupModelModelPlaceholder')"
                    variant="outlined"
                    density="compact"
                    clearable
                    @update:model-value="submitGroupModelDisable"
                  />
                  <!-- 无确认按钮：模型选定即暂存排除，随渠道主保存提交；误排可在此撤销或保存后经记录恢复。
                       auto-select-first：回车优先选中列表匹配项的完整模型名，避免把输入过程中的部分文本（如 "g"/"ge"）暂存成误条目。 -->
                  <div class="text-caption text-medium-emphasis mt-2">{{ t('channelCard.groupModelInlineHint') }}</div>
                  <div v-if="pendingDisablesForEditingKey.length" class="d-flex flex-wrap ga-2 mt-2">
                    <v-chip
                      v-for="pending in pendingDisablesForEditingKey"
                      :key="pending.model"
                      size="small"
                      color="warning"
                      variant="tonal"
                      closable
                      @click:close="unstageGroupModelDisable(pending.key, pending.model)"
                    >
                      <v-icon start size="14">mdi-clock-outline</v-icon>
                      {{ pending.model }}
                    </v-chip>
                  </div>
                </div>
              </v-expand-transition>

              <v-expand-transition>
                <div
                  v-if="row.volcengineCredential && expandedCredentialKey === row.key"
                  class="volcengine-key-detail px-4 pt-3 pb-4"
                >
                  <div class="d-flex align-center justify-space-between ga-3 flex-wrap mb-3">
                    <div class="d-flex align-center ga-2 flex-wrap">
                      <v-chip
                        v-if="row.volcengineCredential.volcenginePlan"
                        size="x-small"
                        color="success"
                        variant="tonal"
                        :href="getVolcenginePlanConsoleURL(row.volcengineCredential.volcenginePlan)"
                        target="_blank"
                        rel="noopener noreferrer"
                        @click.stop
                      >
                        {{ planDisplayName(row.volcengineCredential.volcenginePlan) }}
                        <span v-if="row.volcengineCredential.volcenginePlanTier"> · {{ row.volcengineCredential.volcenginePlanTier }}</span>
                        <v-icon end size="12">mdi-open-in-new</v-icon>
                      </v-chip>
                      <v-chip
                        v-if="row.volcengineCredential.volcengineAccessKeyIdMask"
                        size="x-small"
                        color="info"
                        variant="tonal"
                      >
                        {{ row.volcengineCredential.volcengineAccessKeyIdMask }}
                      </v-chip>
                      <v-chip
                        v-if="!row.volcengineCredential.hasVolcengineAccessKey"
                        size="x-small"
                        color="warning"
                        variant="tonal"
                      >
                        {{ t('volcengineAccessKey.notConfigured') }}
                      </v-chip>
                    </div>
                    <v-btn
                      v-if="row.volcengineCredential.hasVolcengineAccessKey"
                      icon
                      size="x-small"
                      variant="text"
                      :loading="volcengineUsageRefreshing[row.volcengineCredential.credentialUid]"
                      :title="t('volcengineAccessKey.refresh')"
                      @click="refreshVolcengineUsage(row.volcengineCredential)"
                    >
                      <v-icon size="small">mdi-refresh</v-icon>
                    </v-btn>
                  </div>

                  <div v-if="row.volcengineCredential.hasVolcengineAccessKey" class="mb-3">
                    <UsageQuotaRows
                      v-if="hasVolcengineUsageData(row.volcengineCredential.volcenginePlanUsage) || row.volcengineCredential.volcenginePlanUsage?.error"
                      :items="volcengineUsageItems(row.volcengineCredential.volcenginePlanUsage)"
                    />
                    <div v-else class="text-caption text-disabled">
                      {{ row.volcengineCredential.volcenginePlanUsage?.error || t('volcengineAccessKey.noUsageData') }}
                    </div>
                    <div
                      v-if="row.volcengineCredential.volcenginePlanUsage?.fetchedAt && !row.volcengineCredential.volcenginePlanUsage.error"
                      class="text-caption text-disabled mt-2"
                    >
                      {{ t('volcengineAccessKey.usageFetchedAt') }} {{ formatVolcengineTime(row.volcengineCredential.volcenginePlanUsage.fetchedAt) }}
                    </div>
                  </div>

                  <div v-if="otherVolcengineBuckets(row.volcengineCredential).length" class="mb-3">
                    <div class="text-caption text-disabled mb-1">{{ t('volcengineAccessKey.otherPlans') }}</div>
                    <div v-for="bucket in otherVolcengineBuckets(row.volcengineCredential)" :key="`${bucket.product}-${bucket.edition}`" class="d-flex align-center ga-2 flex-wrap mb-1">
                      <v-chip
                        size="x-small"
                        :color="bucket.edition === 'team' ? 'primary' : 'success'"
                        variant="tonal"
                        :href="getVolcenginePlanConsoleURL(bucket.product)"
                        target="_blank"
                        rel="noopener noreferrer"
                        @click.stop
                      >
                        {{ planDisplayName(bucket.product) }}<span v-if="bucket.tier"> · {{ bucket.tier }}</span>
                        <span class="ml-1 opacity-70">· {{ bucket.edition === 'team' ? t('volcengineAccessKey.teamEdition') : t('volcengineAccessKey.personalEdition') }}</span>
                        <v-icon end size="12">mdi-open-in-new</v-icon>
                      </v-chip>
                      <v-chip v-if="bucket.seatId" size="x-small" variant="tonal" color="info">seat {{ bucket.seatId }}</v-chip>
                      <span v-if="bucket.error" class="text-caption text-error">{{ bucket.error }}</span>
                      <span v-else-if="volcengineBucketUsageText(bucket)" class="text-caption">{{ volcengineBucketUsageText(bucket) }}</span>
                      <span v-else class="text-caption text-disabled">{{ t('volcengineAccessKey.noUsageData') }}</span>
                    </div>
                  </div>

                  <div v-if="volcengineForms[row.volcengineCredential.credentialUid]" class="d-flex flex-column ga-2">
                    <div class="volcengine-key-fields">
                      <v-text-field
                        v-model="volcengineForms[row.volcengineCredential.credentialUid].accessKeyId"
                        :label="t('volcengineAccessKey.accessKeyId')"
                        variant="outlined"
                        density="compact"
                        autocomplete="off"
                        hide-details
                      />
                      <v-text-field
                        v-model="volcengineForms[row.volcengineCredential.credentialUid].secretAccessKey"
                        :label="t('volcengineAccessKey.secretAccessKey')"
                        type="password"
                        variant="outlined"
                        density="compact"
                        autocomplete="new-password"
                        hide-details
                      />
                    </div>
                    <v-alert
                      v-if="volcengineForms[row.volcengineCredential.credentialUid].error"
                      color="error"
                      variant="tonal"
                      density="compact"
                    >
                      {{ volcengineForms[row.volcengineCredential.credentialUid].error }}
                    </v-alert>
                    <div class="d-flex align-center justify-end ga-2">
                      <v-btn
                        v-if="row.volcengineCredential.hasVolcengineAccessKey"
                        size="small"
                        variant="text"
                        color="error"
                        :loading="volcengineForms[row.volcengineCredential.credentialUid].clearing"
                        @click="clearVolcengineAccessKey(row.volcengineCredential)"
                      >
                        <v-icon start size="small">mdi-delete-outline</v-icon>
                        {{ t('volcengineAccessKey.clear') }}
                      </v-btn>
                      <v-btn
                        size="small"
                        variant="tonal"
                        color="primary"
                        :loading="volcengineForms[row.volcengineCredential.credentialUid].saving"
                        :disabled="!canSaveVolcengineCredential(row.volcengineCredential.credentialUid)"
                        @click="saveVolcengineAccessKey(row.volcengineCredential)"
                      >
                        <v-icon start size="small">mdi-content-save-outline</v-icon>
                        {{ t('volcengineAccessKey.verifyAndSave') }}
                      </v-btn>
                    </div>
                  </div>
                </div>
              </v-expand-transition>

              <v-expand-transition>
                <div
                  v-if="row.kimiCredential && expandedCredentialKey === row.key"
                  class="volcengine-key-detail px-4 pt-3 pb-4"
                >
                  <div class="d-flex align-center justify-space-between ga-3 flex-wrap mb-3">
                    <v-chip
                      :color="row.kimiCredential.hasKimiConsoleToken ? 'info' : 'warning'"
                      size="x-small"
                      variant="tonal"
                    >
                      {{ row.kimiCredential.hasKimiConsoleToken
                        ? t('kimiConsoleToken.configured')
                        : t('kimiConsoleToken.notConfigured') }}
                    </v-chip>
                    <v-btn
                      v-if="row.kimiCredential.hasKimiConsoleToken"
                      icon
                      size="x-small"
                      variant="text"
                      :loading="kimiForms[row.kimiCredential.credentialUid]?.refreshing"
                      :title="t('kimiConsoleToken.refresh')"
                      @click="refreshKimiToken(row.kimiCredential)"
                    >
                      <v-icon size="small">mdi-refresh</v-icon>
                    </v-btn>
                  </div>

                  <template v-if="row.kimiCredential.kimiCodeUsage">
                    <UsageQuotaRows
                      v-if="buildKimiUsageSections(row.kimiCredential.kimiCodeUsage).length"
                      :items="kimiUsageItems(row.kimiCredential.kimiCodeUsage)"
                      class="mb-3"
                    />
                    <div v-else class="text-caption text-disabled mb-3">{{ t('kimiConsoleToken.noUsageData') }}</div>

                    <div class="text-caption text-disabled mb-3">
                      {{ t('kimiConsoleToken.validatedAt') }} {{ kimiFormatDateTime(row.kimiCredential.kimiCodeUsage.validatedAt) }}
                    </div>
                  </template>
                  <div v-else class="text-caption text-disabled mb-3">{{ t('kimiConsoleToken.noUsageData') }}</div>

                  <div v-if="kimiForms[row.kimiCredential.credentialUid]" class="d-flex flex-column ga-2">
                    <v-text-field
                      v-model="kimiForms[row.kimiCredential.credentialUid].accessToken"
                      :label="t('kimiConsoleToken.token')"
                      :placeholder="t('kimiConsoleToken.tokenPlaceholder')"
                      type="password"
                      variant="outlined"
                      density="compact"
                      autocomplete="new-password"
                      hide-details
                    />
                    <v-alert
                      v-if="kimiForms[row.kimiCredential.credentialUid].error"
                      color="error"
                      variant="tonal"
                      density="compact"
                    >
                      {{ kimiForms[row.kimiCredential.credentialUid].error }}
                    </v-alert>
                    <div class="d-flex align-center justify-end ga-2 flex-wrap">
                      <v-btn
                        v-if="row.kimiCredential.hasKimiConsoleToken"
                        size="small"
                        variant="text"
                        color="error"
                        :loading="kimiForms[row.kimiCredential.credentialUid].clearing"
                        @click="clearKimiToken(row.kimiCredential)"
                      >
                        <v-icon start size="small">mdi-delete-outline</v-icon>
                        {{ t('kimiConsoleToken.clear') }}
                      </v-btn>
                      <v-btn
                        size="small"
                        variant="tonal"
                        color="primary"
                        :loading="kimiForms[row.kimiCredential.credentialUid].saving"
                        :disabled="!kimiForms[row.kimiCredential.credentialUid].accessToken.trim()"
                        @click="saveKimiToken(row.kimiCredential)"
                      >
                        <v-icon start size="small">mdi-check-decagram-outline</v-icon>
                        {{ t('kimiConsoleToken.verifyAndSave') }}
                      </v-btn>
                    </div>
                  </div>
                </div>
              </v-expand-transition>

              <v-expand-transition>
                <div
                  v-if="row.mimoCredential && expandedCredentialKey === row.key"
                  class="volcengine-key-detail px-4 pt-3 pb-4"
                >
                  <div class="d-flex align-center ga-2 flex-wrap mb-3">
                    <v-chip v-if="row.mimoCredential.mimoTokenPlan" size="x-small" color="success" variant="tonal">
                      {{ row.mimoCredential.mimoTokenPlan.planName }}
                    </v-chip>
                    <v-chip
                      :color="row.mimoCredential.hasMiMoConsoleCookie ? 'info' : 'warning'"
                      size="x-small"
                      variant="tonal"
                    >
                      {{ row.mimoCredential.hasMiMoConsoleCookie
                        ? t('mimoConsoleCookie.configured')
                        : t('mimoConsoleCookie.notConfigured') }}
                    </v-chip>
                  </div>
                  <UsageQuotaRows
                    v-if="row.mimoCredential.mimoTokenPlan"
                    :items="mimoUsageItems(row.mimoCredential.mimoTokenPlan)"
                    class="mb-3"
                  />
                  <div v-else class="text-caption text-disabled mb-3">{{ t('mimoConsoleCookie.noUsageData') }}</div>
                  <div v-if="mimoForms[row.mimoCredential.credentialUid]" class="d-flex flex-column ga-2">
                    <v-text-field
                      v-model="mimoForms[row.mimoCredential.credentialUid].cookie"
                      :label="t('mimoConsoleCookie.cookie')"
                      :placeholder="t('mimoConsoleCookie.cookiePlaceholder')"
                      type="password"
                      variant="outlined"
                      density="compact"
                      autocomplete="new-password"
                      hide-details
                    />
                    <v-alert
                      v-if="mimoForms[row.mimoCredential.credentialUid].error"
                      color="error"
                      variant="tonal"
                      density="compact"
                    >
                      {{ mimoForms[row.mimoCredential.credentialUid].error }}
                    </v-alert>
                    <div class="d-flex align-center justify-end ga-2 flex-wrap">
                      <v-btn
                        v-if="row.mimoCredential.hasMiMoConsoleCookie"
                        size="small"
                        variant="text"
                        color="secondary"
                        :loading="mimoForms[row.mimoCredential.credentialUid].refreshing"
                        @click="refreshMiMoConsoleCookie(row.mimoCredential)"
                      >
                        <v-icon start size="small">mdi-refresh</v-icon>
                        {{ t('mimoConsoleCookie.refresh') }}
                      </v-btn>
                      <v-btn
                        v-if="row.mimoCredential.hasMiMoConsoleCookie"
                        size="small"
                        variant="text"
                        color="error"
                        :loading="mimoForms[row.mimoCredential.credentialUid].clearing"
                        @click="clearMiMoConsoleCookie(row.mimoCredential)"
                      >
                        <v-icon start size="small">mdi-delete-outline</v-icon>
                        {{ t('mimoConsoleCookie.clear') }}
                      </v-btn>
                      <v-btn
                        size="small"
                        variant="tonal"
                        color="primary"
                        :loading="mimoForms[row.mimoCredential.credentialUid].saving"
                        :disabled="!mimoForms[row.mimoCredential.credentialUid].cookie.trim()"
                        @click="saveMiMoConsoleCookie(row.mimoCredential)"
                      >
                        <v-icon start size="small">mdi-check-decagram-outline</v-icon>
                        {{ t('mimoConsoleCookie.verifyAndSave') }}
                      </v-btn>
                    </div>
                  </div>
                </div>
              </v-expand-transition>

              <v-expand-transition>
                <div
                  v-if="row.compshareCredential && expandedCredentialKey === row.key"
                  class="volcengine-key-detail px-4 pt-3 pb-4"
                >
                  <div class="d-flex align-center justify-space-between ga-3 flex-wrap mb-3">
                    <div class="d-flex align-center ga-2 flex-wrap">
                      <v-chip
                        v-if="row.compshareCredential.compsharePlan"
                        :color="row.compshareCredential.compsharePlan.status === 1 ? 'success' : 'error'"
                        size="x-small"
                        variant="tonal"
                      >
                        {{ compsharePlanDisplayName(row.compshareCredential.compsharePlan) }}
                      </v-chip>
                      <v-chip
                        :color="row.compshareCredential.hasCompshareConsoleCookie ? 'info' : 'warning'"
                        size="x-small"
                        variant="tonal"
                      >
                        {{ row.compshareCredential.hasCompshareConsoleCookie
                          ? t('compshareConsoleCookie.configured')
                          : t('compshareConsoleCookie.notConfigured') }}
                      </v-chip>
                    </div>
                    <v-btn
                      v-if="row.compshareCredential.hasCompshareConsoleCookie"
                      icon
                      size="x-small"
                      variant="text"
                      :loading="compshareForms[row.compshareCredential.credentialUid]?.refreshing"
                      :title="t('compshareConsoleCookie.refresh')"
                      @click="refreshCompshareCookie(row.compshareCredential)"
                    >
                      <v-icon size="small">mdi-refresh</v-icon>
                    </v-btn>
                  </div>

                  <template v-if="row.compshareCredential.compsharePlan">
                    <UsageQuotaRows
                      :items="compshareUsageQuotaItems(row.compshareCredential.compsharePlan)"
                      class="mb-3"
                    />
                    <div class="compshare-plan-meta mb-2">
                      <div>
                        <div class="text-caption text-medium-emphasis">{{ t('compshareConsoleCookie.concurrency') }}</div>
                        <div class="text-body-2 font-weight-medium">{{ row.compshareCredential.compsharePlan.concurrencyLimit }}</div>
                      </div>
                      <div>
                        <div class="text-caption text-medium-emphasis">{{ t('compshareConsoleCookie.accountType') }}</div>
                        <div class="text-body-2 font-weight-medium">
                          {{ row.compshareCredential.compsharePlan.isTeam
                            ? t('compshareConsoleCookie.team')
                            : t('compshareConsoleCookie.personal') }}
                        </div>
                      </div>
                      <div>
                        <div class="text-caption text-medium-emphasis">{{ t('compshareConsoleCookie.expiresAt') }}</div>
                        <div class="text-body-2 font-weight-medium">{{ compshareFormatEpoch(row.compshareCredential.compsharePlan.expireAt) }}</div>
                      </div>
                    </div>
                    <div class="text-caption text-disabled mb-3">
                      {{ t('compshareConsoleCookie.validatedAt') }} {{ compshareFormatDateTime(row.compshareCredential.compsharePlan.validatedAt) }}
                    </div>
                  </template>
                  <div v-else class="text-caption text-disabled mb-3">{{ t('compshareConsoleCookie.noUsageData') }}</div>

                  <div v-if="compshareForms[row.compshareCredential.credentialUid]" class="d-flex flex-column ga-2">
                    <v-text-field
                      v-model="compshareForms[row.compshareCredential.credentialUid].cookie"
                      :label="t('compshareConsoleCookie.cookie')"
                      :placeholder="t('compshareConsoleCookie.cookiePlaceholder')"
                      type="password"
                      variant="outlined"
                      density="compact"
                      autocomplete="new-password"
                      hide-details
                    />
                    <v-alert
                      v-if="compshareForms[row.compshareCredential.credentialUid].error"
                      color="error"
                      variant="tonal"
                      density="compact"
                    >
                      {{ compshareForms[row.compshareCredential.credentialUid].error }}
                    </v-alert>
                    <div class="d-flex align-center justify-end ga-2 flex-wrap">
                      <v-btn
                        v-if="row.compshareCredential.hasCompshareConsoleCookie"
                        size="small"
                        variant="text"
                        color="error"
                        :loading="compshareForms[row.compshareCredential.credentialUid].clearing"
                        @click="clearCompshareCookie(row.compshareCredential)"
                      >
                        <v-icon start size="small">mdi-delete-outline</v-icon>
                        {{ t('compshareConsoleCookie.clear') }}
                      </v-btn>
                      <v-btn
                        size="small"
                        variant="tonal"
                        color="primary"
                        :loading="compshareForms[row.compshareCredential.credentialUid].saving"
                        :disabled="!compshareForms[row.compshareCredential.credentialUid].cookie.trim()"
                        @click="saveCompshareCookie(row.compshareCredential)"
                      >
                        <v-icon start size="small">mdi-check-decagram-outline</v-icon>
                        {{ t('compshareConsoleCookie.verifyAndSave') }}
                      </v-btn>
                    </div>
                  </div>
                </div>
              </v-expand-transition>

              <v-expand-transition>
                <div
                  v-if="row.minimaxEndpoint && expandedCredentialKey === row.key"
                  class="volcengine-key-detail px-4 pt-3 pb-4"
                >
                  <div class="d-flex align-center justify-space-between ga-3 flex-wrap mb-3">
                    <v-chip size="x-small" color="primary" variant="tonal">Token Plan</v-chip>
                    <v-btn
                      icon
                      size="x-small"
                      variant="text"
                      :loading="minimaxUsageRefreshing[row.minimaxEndpoint.endpointUid]"
                      :title="t('healthCenter.detail.refreshUsage')"
                      @click="refreshMinimaxUsage(row.minimaxEndpoint)"
                    >
                      <v-icon size="small">mdi-refresh</v-icon>
                    </v-btn>
                  </div>

                  <div
                    v-if="row.minimaxEndpoint.miniMaxTokenPlanUsage?.models.length && !row.minimaxEndpoint.miniMaxTokenPlanUsageError"
                    class="mb-2"
                  >
                    <UsageQuotaRows :items="minimaxQuotaItems(row.minimaxEndpoint)" />
                    <div class="text-caption text-disabled mt-2">
                      {{ t('healthCenter.detail.updatedAt') }}: {{ minimaxFormatDateTime(row.minimaxEndpoint.miniMaxTokenPlanUsage.fetchedAt) }}
                    </div>
                  </div>
                  <div v-else-if="row.minimaxEndpoint.miniMaxTokenPlanUsageError" class="text-caption text-error">
                    {{ t('healthCenter.detail.tokenPlanUsageError') }}: {{ row.minimaxEndpoint.miniMaxTokenPlanUsageError }}
                  </div>
                  <div v-else class="text-caption text-disabled">{{ t('healthCenter.detail.noUsageData') }}</div>
                </div>
              </v-expand-transition>
            </div>
              </template>
            </draggable>
          </v-list>
        </div>

        <div v-if="providerId === 'deepseek' && accountUid" class="deepseek-balance mb-5">
          <v-divider class="mb-4" />
          <div class="d-flex align-center justify-space-between ga-3 flex-wrap mb-2">
            <div class="d-flex align-center ga-2">
              <v-icon color="primary" size="small">mdi-wallet-outline</v-icon>
              <span class="text-body-2 font-weight-medium">{{ t('deepseekBalance.title') }}</span>
            </div>
            <v-btn
              icon
              size="small"
              variant="text"
              color="primary"
              :loading="deepseekBalancesLoading"
              :title="t('deepseekBalance.refresh')"
              @click="loadDeepSeekBalances"
            >
              <v-icon size="small">mdi-refresh</v-icon>
            </v-btn>
          </div>
          <div class="text-caption text-medium-emphasis mb-3">{{ t('deepseekBalance.hint') }}</div>
          <v-progress-linear v-if="deepseekBalancesLoading" indeterminate color="primary" class="mb-3" />
          <v-alert v-if="deepseekBalancesError" color="error" variant="tonal" density="compact" class="mb-3">
            {{ deepseekBalancesError }}
          </v-alert>
          <div
            v-for="credential in deepseekBalances"
            :key="credential.credentialUid"
            class="deepseek-credential py-3"
          >
            <div class="d-flex align-center justify-space-between ga-3 flex-wrap mb-2">
              <code class="text-caption">{{ credential.keyMask }}</code>
              <v-chip
                size="x-small"
                variant="tonal"
                :color="credential.error ? 'error' : credential.isAvailable ? 'success' : 'warning'"
              >
                {{ credential.error
                  ? t('deepseekBalance.queryFailed')
                  : credential.isAvailable
                    ? t('deepseekBalance.available')
                    : t('deepseekBalance.unavailable') }}
              </v-chip>
            </div>
            <v-alert v-if="credential.error" color="error" variant="tonal" density="compact">
              {{ credential.error }}
            </v-alert>
            <div v-else-if="credential.balanceInfos?.length" class="deepseek-balance-grid">
              <div v-for="balance in credential.balanceInfos" :key="balance.currency" class="deepseek-balance-currency">
                <div class="text-caption font-weight-medium mb-2">{{ balance.currency }}</div>
                <div class="deepseek-balance-values">
                  <div>
                    <div class="text-caption text-medium-emphasis">{{ t('deepseekBalance.total') }}</div>
                    <div class="text-body-2 font-weight-medium">{{ balance.totalBalance }}</div>
                  </div>
                  <div>
                    <div class="text-caption text-medium-emphasis">{{ t('deepseekBalance.granted') }}</div>
                    <div class="text-body-2 font-weight-medium">{{ balance.grantedBalance }}</div>
                  </div>
                  <div>
                    <div class="text-caption text-medium-emphasis">{{ t('deepseekBalance.toppedUp') }}</div>
                    <div class="text-body-2 font-weight-medium">{{ balance.toppedUpBalance }}</div>
                  </div>
                </div>
              </div>
            </div>
            <div v-else class="text-caption text-disabled">{{ t('deepseekBalance.noBalance') }}</div>
          </div>
        </div>

        <!-- 添加新密钥 -->
        <div class="d-flex align-start ga-3">
          <v-text-field
            v-model="newApiKey"
            :label="t('addChannel.addNewApiKey')"
            :placeholder="t('channelEditor.auth.addNewApiKey.placeholder')"
            prepend-inner-icon="mdi-plus"
            variant="outlined"
            density="comfortable"
            type="password"
            :error="!!apiKeyError"
            :error-messages="apiKeyError"
            class="flex-grow-1"
            @keyup.enter="handleAddKey"
            @input="handleInput"
          />
          <v-btn
            color="primary"
            variant="elevated"
            size="large"
            height="40"
            :disabled="!newApiKey.trim()"
            class="mt-1"
            @click="handleAddKey"
          >
            {{ t('app.actions.add') }}
          </v-btn>
        </div>

        <CopilotKeyAuthorizationPanel
          :api-keys="apiKeys"
          :proxy-url="proxyUrl"
          :service-type="serviceType"
          :is-editing="isEditing"
          :channel-id="channelId"
          @update:api-keys="$emit('update:apiKeys', $event)"
          @update:proxy-url="$emit('update:proxyUrl', $event)"
        />

        <!-- 人工配额分组模型策略，与自动临时限制分开呈现 -->
        <div v-if="isEditing && visibleDisabledGroupModels.length" class="mt-4">
          <div class="d-flex align-center ga-2 mb-2">
            <v-icon size="small" color="secondary">mdi-tune-variant</v-icon>
            <span class="text-body-2 font-weight-medium">{{ t('channelCard.disabledGroupModels') }}</span>
            <v-chip size="x-small" color="secondary" variant="tonal">{{ visibleDisabledGroupModels.length }}</v-chip>
          </div>
          <v-list density="compact" class="rounded-lg group-model-policy-list">
            <v-list-item
              v-for="record in visibleDisabledGroupModels"
              :key="groupModelPolicyKey(record)"
              class="px-3"
            >
              <template #prepend><v-icon size="small" color="secondary" class="mr-2">mdi-account-multiple-outline</v-icon></template>
              <v-list-item-title class="d-flex align-center ga-2 flex-wrap text-caption">
                <v-chip size="x-small" color="secondary" variant="tonal">{{ record.quotaGroup?.trim() || t('channelCard.independentKey') }}</v-chip>
                <code v-if="!record.quotaGroup?.trim() && record.key">{{ maskApiKey(record.key) }}</code>
                <strong>{{ record.model }}</strong>
              </v-list-item-title>
              <v-list-item-subtitle class="text-caption">
                {{ t('channelCard.groupModelManualNote') }} · {{ formatDisabledTime(record.disabledAt) }}
              </v-list-item-subtitle>
              <template #append>
                <v-btn
                  size="x-small"
                  color="success"
                  variant="tonal"
                  :loading="changingGroupModel === groupModelPolicyKey(record)"
                  :disabled="!!changingGroupModel"
                  @click="$emit('restore-group-model', record)"
                >
                  <v-icon start size="small">mdi-restore</v-icon>{{ t('channelCard.restoreGroupModel') }}
                </v-btn>
              </template>
            </v-list-item>
          </v-list>
        </div>

        <!-- 被限制的 (Key, 模型) 组合（仅编辑模式，自动临时限制） -->
        <div v-if="isEditing && visibleDisabledKeyModels.length" class="mt-4">
          <div class="d-flex align-center ga-2 mb-2">
            <v-icon size="small" color="warning">mdi-alert-circle-outline</v-icon>
            <span class="text-body-2 font-weight-medium text-warning">{{ t('channelCard.disabledKeyModels') }}</span>
            <v-chip size="x-small" color="warning" variant="tonal">{{ visibleDisabledKeyModels.length }}</v-chip>
          </div>
          <v-list density="compact" class="rounded-lg" style="max-height: 150px; overflow-y: auto;">
            <v-list-item
              v-for="(dm, dmIdx) in visibleDisabledKeyModels"
              :key="'disabled-model-' + dmIdx"
              class="px-3"
              style="background: rgba(var(--v-theme-warning), 0.04);"
            >
              <template #prepend>
                <v-icon size="small" color="warning" class="mr-2">mdi-key-alert</v-icon>
              </template>
              <v-list-item-title class="text-caption font-weight-mono">
                {{ dm.key.length > 20 ? dm.key.slice(0, 8) + '***' + dm.key.slice(-5) : dm.key }}
                <v-chip size="x-small" color="primary" variant="tonal" class="ml-1">{{ dm.model }}</v-chip>
              </v-list-item-title>
              <v-list-item-subtitle class="d-flex align-center ga-1">
                <v-chip size="x-small" color="warning" variant="tonal">{{ t(getDisabledKeyLabel(dm.reason)) }}</v-chip>
                <span class="text-caption">{{ t('channelCard.recoverAt') }}: {{ new Date(dm.recoverAt).toLocaleString() }}</span>
              </v-list-item-subtitle>
              <template #append>
                <v-btn
                  size="x-small"
                  color="success"
                  variant="tonal"
                  rounded="lg"
                  :loading="restoringKeyModel === (dm.key + '|' + dm.model)"
                  :disabled="!!restoringKeyModel"
                  @click="$emit('restore-key-model', dm.key, dm.model)"
                >
                  <v-icon start size="small">mdi-restore</v-icon>
                  {{ t('channelCard.restoreKey') }}
                </v-btn>
              </template>
            </v-list-item>
          </v-list>
        </div>
      </v-card-text>
    </v-card>

  </div>
</template>

<script setup lang="ts">
import { ref, computed, onBeforeUnmount, watch } from 'vue'
import { useI18n } from '../../../i18n'
import { ApiService } from '../../../services/api'
import type {
  DisabledKeyInfo,
  DisabledGroupModelInfo,
  EndpointDetailItem,
  ManagedAccountCredential,
  APIKeyConfig,
} from '../../../services/api-types'
import draggable from 'vuedraggable'
import { maskApiKey } from '../../../utils/apiKeyMask'
import { buildChannelApiKeyRows, groupModelPolicyKey, type ChannelApiKeyRow } from '../../../utils/channelApiKeys'
import { getVolcenginePlanConsoleURL } from '../../../utils/channelWebsite'
import { buildKimiUsageSections } from '../../../utils/kimiPlanUsage'
import { selectMiniMaxTokenPlanEndpoint, sha256KeyHash } from '../../../utils/minimaxEndpointUsage'
import { multiplierStatusLabel } from '../../../utils/subscriptionBilling'
import UsageQuotaRows from '../../../components/UsageQuotaRows.vue'
import CopilotKeyAuthorizationPanel from './CopilotKeyAuthorizationPanel.vue'
import { useKeyPlanUsage } from './useKeyPlanUsage'
import { useManagedKeyCredentials } from './useManagedKeyCredentials'

interface KeyModelsStatus {
  loading?: boolean
  success?: boolean
  error?: string
  statusCode?: string | number
  modelCount?: number
}

interface DisabledKeyModel {
  key: string
  model: string
  reason: string
  disabledAt: string
  recoverAt: string
}

interface Props {
  apiKeys: string[]
  disabledKeys: DisabledKeyInfo[]
  disabledKeyModels?: DisabledKeyModel[]
  disabledGroupModels?: DisabledGroupModelInfo[]
  pendingGroupModelDisables?: Array<{ key: string; model: string; note?: string }>
  modelOptions?: Array<{ title: string; value: string }>
  apiKeyConfigs?: APIKeyConfig[]
  keyModelsStatus: Map<string, KeyModelsStatus>
  isEditing: boolean
  restoringKey: string
  restoringKeyModel?: string
  changingGroupModel?: string
  removingKey?: string
  suspendingKey?: string
  serviceType?: string
  channelId?: number
  channelUid?: string
  channelKind?: 'messages' | 'chat' | 'responses' | 'gemini' | 'images' | 'vectors'
  channelMaxGroupMultiplier?: number | null
  dialogOpen: boolean
  proxyUrl?: string
  accountUid?: string
  providerId?: string
}

const props = defineProps<Props>()

const emit = defineEmits<{
  'update:apiKeys': [string[]]
  'update:apiKeyConfigs': [APIKeyConfig[]]
  'update:proxyUrl': [string]
  'restore-key': [string]
  'restore-key-model': [string, string]
  'stage-group-model-disable': [string, string]
  'unstage-group-model-disable': [string, string]
  'restore-group-model': [DisabledGroupModelInfo]
  'remove-key': [string]
  'suspend-key': [string]
  'resume-key': [string]
  'ensure-models-loaded': []
}>()

const { t } = useI18n()
const apiService = new ApiService()
const expandedCredentialKey = ref<string | null>(null)
const {
  deepseekBalances,
  deepseekBalancesLoading,
  deepseekBalancesError,
  volcengineCredentials,
  volcengineCredentialsLoading,
  volcengineCredentialsError,
  volcengineForms,
  volcengineUsageRefreshing,
  kimiCredentials,
  kimiCredentialsLoading,
  kimiCredentialsError,
  kimiForms,
  mimoCredentials,
  mimoCredentialsLoading,
  mimoCredentialsError,
  mimoForms,
  compshareCredentials,
  compshareCredentialsLoading,
  compshareCredentialsError,
  compshareForms,
  loadDeepSeekBalances,
  saveCompshareCookie,
  refreshCompshareCookie,
  clearCompshareCookie,
  saveMiMoConsoleCookie,
  refreshMiMoConsoleCookie,
  clearMiMoConsoleCookie,
  refreshVolcengineUsage,
  canSaveVolcengineCredential,
  saveVolcengineAccessKey,
  clearVolcengineAccessKey,
  saveKimiToken,
  refreshKimiToken,
  clearKimiToken,
} = useManagedKeyCredentials(props, keys => emit('update:apiKeys', keys))
const {
  compshareUsageQuotaItems,
  compshareFormatEpoch,
  compshareFormatDateTime,
  compsharePlanDisplayName,
  compshareUsageSummary,
  mimoUsageSummary,
  minimaxQuotaItems,
  minimaxFormatDateTime,
  minimaxUsageSummary,
  mimoUsageItems,
  planDisplayName,
  hasVolcengineUsageData,
  volcengineUsageItems,
  volcengineUsageSummaryParts,
  formatVolcengineTime,
  otherVolcengineBuckets,
  volcengineBucketUsageText,
  kimiUsageItems,
  kimiFormatDateTime,
  kimiUsageSummaryParts,
} = useKeyPlanUsage()

const newApiKey = ref('')
const apiKeyError = ref('')
const duplicateKeyIndex = ref<number | null>(null)
const copiedKey = ref('')
const groupModelEditing = ref<ChannelApiKeyRow | null>(null)
const groupModelForm = ref({ model: '' })
// Key 行统一详情展开（倍率 + 分组模型排除同一块）：一次只展开一行，切换即重置编辑态。
const expandedDetailKey = ref<string | null>(null)
const multiplierEditing = ref<ChannelApiKeyRow | null>(null)
const multiplierForm = ref<{ groupMultiplier: number | string | null; consumptionPolicy: 'normal' | 'opportunistic' | null }>({ groupMultiplier: null, consumptionPolicy: null })

const consumptionPolicyOptions = computed(() => [
  { title: t('subscription.keyMultiplier.policyNormal'), value: 'normal' as const },
  { title: t('subscription.keyMultiplier.policyOpportunistic'), value: 'opportunistic' as const },
])
const minimaxEndpoints = ref<EndpointDetailItem[]>([])
const minimaxEndpointsLoading = ref(false)
const minimaxEndpointsError = ref('')
const minimaxUsageRefreshing = ref<Record<string, boolean>>({})
const minimaxKeyHashes = ref<Record<string, string>>({})
let minimaxEndpointsRequestId = 0
let minimaxKeyHashRequestId = 0
const keyRows = computed(() => buildChannelApiKeyRows(props.apiKeys, props.disabledKeys, props.apiKeyConfigs).map(row => {
  const matchCredential = (credentials: ManagedAccountCredential[]) =>
    credentials.find(credential => credential.keyMask === maskApiKey(row.key))
  const volcengineCredential = props.providerId === 'volcengine' ? matchCredential(volcengineCredentials.value) : undefined
  const kimiCredential = props.providerId === 'kimi' ? matchCredential(kimiCredentials.value) : undefined
  const mimoCredential = props.providerId === 'mimo' ? matchCredential(mimoCredentials.value) : undefined
  const compshareCredential = props.providerId === 'compshare' ? matchCredential(compshareCredentials.value) : undefined
  const minimaxEndpoint = props.providerId === 'minimax'
    ? selectMiniMaxTokenPlanEndpoint(minimaxEndpoints.value, minimaxKeyHashes.value[row.key] ?? '', maskApiKey(row.key))
    : undefined
  return {
    ...row,
    volcengineCredential,
    kimiCredential,
    mimoCredential,
    compshareCredential,
    minimaxEndpoint,
    planCredential: volcengineCredential ?? kimiCredential ?? mimoCredential ?? compshareCredential,
    consumptionPolicy: row.consumptionPolicy,
    effectiveCostClass: row.effectiveCostClass,
  }
}))

// 可排序的活跃 Key 行：拖拽与置顶/置底只作用于活跃 Key，拉黑 Key 固定展示在下方。
const activeKeyRows = computed(() => keyRows.value.filter(row => !row.disabled))

const sortableKeyRows = computed({
  get: () => keyRows.value,
  set: (newRows: ChannelApiKeyRow[]) => {
    const activeKeys = newRows
      .filter(row => row.activeIndex >= 0)
      .map(row => row.key)
    updateApiKeyOrder(activeKeys)
  },
})

const canReorderKeys = computed(() => activeKeyRows.value.length > 1)

const isFirstActiveKey = (row: ChannelApiKeyRow): boolean =>
  activeKeyRows.value[0]?.key === row.key
const isLastActiveKey = (row: ChannelApiKeyRow): boolean =>
  activeKeyRows.value[activeKeyRows.value.length - 1]?.key === row.key

// 统一更新 apiKeys 与 apiKeyConfigs 顺序，保证 Key 与配置项一一对应。
function updateApiKeyOrder(newKeys: string[]) {
  const oldKeys = props.apiKeys
  if (newKeys.length !== oldKeys.length) return

  const configByKey = new Map<string, APIKeyConfig>()
  for (const cfg of props.apiKeyConfigs ?? []) {
    if (cfg.key) configByKey.set(cfg.key, cfg)
  }

  emit('update:apiKeys', newKeys)

  if (props.apiKeyConfigs !== undefined && props.apiKeyConfigs.length > 0) {
    const reorderedConfigs = newKeys
      .map(key => configByKey.get(key))
      .filter((cfg): cfg is APIKeyConfig => !!cfg)
    emit('update:apiKeyConfigs', reorderedConfigs)
  }
}

const moveKeyToTop = (row: ChannelApiKeyRow) => {
  if (!canReorderKeys.value || isFirstActiveKey(row)) return
  const keys = props.apiKeys.filter(key => key !== row.key)
  keys.unshift(row.key)
  updateApiKeyOrder(keys)
}

const moveKeyToBottom = (row: ChannelApiKeyRow) => {
  if (!canReorderKeys.value || isLastActiveKey(row)) return
  const keys = props.apiKeys.filter(key => key !== row.key)
  keys.push(row.key)
  updateApiKeyOrder(keys)
}

watch(
  () => buildChannelApiKeyRows(props.apiKeys, props.disabledKeys).map(row => row.key),
  async keys => {
    const requestId = ++minimaxKeyHashRequestId
    const entries = await Promise.all(keys.map(async key => {
      try {
        return [key, await sha256KeyHash(key)] as const
      } catch {
        return [key, ''] as const
      }
    }))
    if (requestId === minimaxKeyHashRequestId) minimaxKeyHashes.value = Object.fromEntries(entries)
  },
  { immediate: true },
)

const planRowTitle = (): string => {
  switch (props.providerId) {
    case 'volcengine': return t('volcengineAccessKey.usageTitle')
    case 'kimi': return t('kimiConsoleToken.title')
    case 'mimo': return t('mimoConsoleCookie.title')
    case 'compshare': return t('compshareConsoleCookie.title')
    case 'minimax': return t('healthCenter.detail.tokenPlanUsage')
    default: return ''
  }
}

const hasConfigurableKeys = computed(() => props.serviceType === 'copilot' || keyRows.value.length > 0)

const visibleDisabledKeyModels = computed(() => props.disabledKeyModels || [])
const visibleDisabledGroupModels = computed(() => props.disabledGroupModels || [])
const modelOptions = computed(() => props.modelOptions || [])
const editingQuotaGroup = computed(() => keyRows.value.find(row => row.key === expandedDetailKey.value)?.quotaGroup?.trim() || '')
const groupModelAffectedCount = computed(() => {
  if (!expandedDetailKey.value) return 0
  const group = editingQuotaGroup.value
  if (!group) return 1
  return keyRows.value.filter(row => row.quotaGroup?.trim() === group && row.activeIndex >= 0).length
})

const multiplierStatusColor = (status?: string) => status === 'fresh' || status === 'manual' ? 'success' : status === 'over_limit' || status === 'sync_error' || status === 'relink_required' ? 'error' : 'warning'

const openMultiplierEditor = (row: ChannelApiKeyRow) => {
  multiplierEditing.value = row
  const policy: 'normal' | 'opportunistic' | null = row.consumptionPolicy === 'opportunistic' ? 'opportunistic' : row.consumptionPolicy === 'normal' ? 'normal' : null
  multiplierForm.value = {
    groupMultiplier: row.groupMultiplier ?? null,
    consumptionPolicy: policy,
  }
}

// Key 行统一详情展开（替代旧弹窗与两个独立入口）：一次只展开一行，切换即重置编辑态。
// 面板同时承载 Key 倍率（暂存表单，随渠道主保存落盘）与分组模型排除（暂存，主保存后提交）。
const toggleKeyDetail = (row: ChannelApiKeyRow) => {
  if (expandedDetailKey.value === row.key) {
    closeKeyDetail()
    return
  }
  openMultiplierEditor(row)
  groupModelEditing.value = row
  groupModelForm.value = { model: '' }
  emit('ensure-models-loaded')
  expandedDetailKey.value = row.key
}

const closeKeyDetail = () => {
  expandedDetailKey.value = null
  multiplierEditing.value = null
  groupModelEditing.value = null
}

// 分组倍率输入框 hint：仅在渠道启用上限时提示（未启用时不解释闸门语义，避免噪声）。
const channelMaxGroupMultiplierHint = computed(() => {
  const limit = Number(props.channelMaxGroupMultiplier)
  if (!Number.isFinite(limit) || limit <= 0) return ''
  return t('subscription.keyMultiplier.channelLimitHint', { limit })
})

// 倍率改动不即时落盘：写入外层渠道编辑表单的 apiKeyConfigs 快照，
// 随渠道主保存一并提交（自定义渠道走渠道 PUT 的 apiKeyConfigs merge；
// 托管渠道由 channel.ts 单卡更新补发）。行副标题 chips 经 props 回流即时反映新值。
const applyMultiplierToConfigs = () => {
  const row = multiplierEditing.value
  if (!row) return
  let groupMultiplier: number | null
  try {
    groupMultiplier = parseMultiplierInput(multiplierForm.value.groupMultiplier)
  } catch {
    return // 非法输入暂不暂存，保留上次合法值
  }
  // 裸 key（从未有过任何 key 级附加配置）会被后端视图的 IsAPIKeyConfigEffective
  // 过滤，表单 apiKeyConfigs 里没有它的条目：首次设置倍率时须补建条目暂存，
  // 否则改动无处可写，随渠道保存后静默丢失。
  let matched = false
  const configs = (props.apiKeyConfigs ?? []).map(cfg => {
    const cfgId = cfg.keyUid ?? cfg.credentialUid
    if (cfgId !== row.keyUid && cfg.key !== row.key) return cfg
    matched = true
    return {
      ...cfg,
      groupMultiplier,
      maxGroupMultiplier: null,
      consumptionPolicy: multiplierForm.value.consumptionPolicy ?? undefined,
    }
  })
  if (!matched) {
    // row.keyUid 可能是 credentialUid 兜底值（buildChannelApiKeyRows 合并过），
    // 与 credentialUid 相同时不写入 keyUid，避免身份字段错位。
    const keyUidIsCredentialFallback = !!row.credentialUid && row.keyUid === row.credentialUid
    configs.push({
      key: row.key,
      ...(row.keyUid && !keyUidIsCredentialFallback ? { keyUid: row.keyUid } : {}),
      ...(row.credentialUid ? { credentialUid: row.credentialUid } : {}),
      groupMultiplier,
      consumptionPolicy: multiplierForm.value.consumptionPolicy ?? undefined,
    })
  }
  emit('update:apiKeyConfigs', configs)
}

// 快捷键保存发生在 window keydown 监听中，可能早于数字输入框的 blur/change。
// 先从当前 input 读取原始值，确保 Ctrl/Command+Enter 保存时使用焦点内的最新倍率。
const flushMultiplierInput = (event: KeyboardEvent) => {
  const target = event.target as { type?: string; value?: string } | null
  if (target?.type === 'number') multiplierForm.value.groupMultiplier = target.value ?? ''
  applyMultiplierToConfigs()
}

const parseMultiplierInput = (value: number | string | null): number | null => {
  if (value === null || (typeof value === 'string' && value.trim() === '')) return null
  const parsed = typeof value === 'number' ? value : Number(value)
  if (!Number.isFinite(parsed) || parsed < 0) throw new Error('倍率必须是有限且非负数')
  return parsed
}

// 模型选定（combobox 选择/手输回车定稿）即暂存排除（不即时调端点），
// 随渠道主保存成功后统一提交；暂存条目在面板中可撤销。清空（clearable → null）不触发。
const submitGroupModelDisable = (model: unknown = groupModelForm.value.model) => {
  const row = groupModelEditing.value
  const trimmed = (model ?? '').toString().trim()
  if (!row || !trimmed) return
  emit('stage-group-model-disable', row.key, trimmed)
  groupModelForm.value = { model: '' }
}

const unstageGroupModelDisable = (key: string, model: string) => {
  emit('unstage-group-model-disable', key, model)
}

// 当前展开 key 的暂存排除条目（展示于面板下半区，随主保存提交）。
const pendingDisablesForEditingKey = computed(() =>
  (props.pendingGroupModelDisables || []).filter(item => item.key === groupModelEditing.value?.key)
)

const toggleCredentialKey = (key: string) => {
  expandedCredentialKey.value = expandedCredentialKey.value === key ? null : key
}

// Key 行点击：整条可点按「展开/收起」优先级分发——可编辑渠道优先开倍率详情，
// 否则回退到套餐用量详情；无详情可开则忽略。其他列表（newapi 账号）同款「点击行即展开」心智。
const onKeyRowClick = (row: ChannelApiKeyRow) => {
  if (row.disabled) return
  // provider 有套餐/用量凭证且非禁用：展开用量详情（保持原「点击行切凭证详情」）
  if (row.planCredential || row.minimaxEndpoint) {
    toggleCredentialKey(row.key)
    return
  }
  // 可编辑渠道普通 Key：展开倍率/分组模型详情
  if (!row.disabled && row.keyUid && props.channelUid && props.channelKind) {
    toggleKeyDetail(row)
  }
}

const disabledKeyColor = (reason: string) => (
  reason === 'insufficient_balance' || reason === 'insufficient_quota' ? 'warning' : 'error'
)

const formatDisabledTime = (iso: string) => {
  const date = new Date(iso)
  return Number.isNaN(date.getTime()) ? iso : date.toLocaleString()
}

const loadMiniMaxEndpoints = async (requestId: number, channelUid: string): Promise<boolean> => {
  const response = await apiService.getHealthCenterChannelEndpoints(channelUid)
  if (requestId !== minimaxEndpointsRequestId) return false
  minimaxEndpoints.value = response.endpoints
  minimaxEndpointsError.value = ''
  return true
}

watch(
  () => [props.dialogOpen, props.providerId, props.channelUid] as const,
  async ([dialogOpen, providerId, channelUid]) => {
    const requestId = ++minimaxEndpointsRequestId
    minimaxEndpoints.value = []
    minimaxEndpointsError.value = ''
    minimaxEndpointsLoading.value = false
    minimaxUsageRefreshing.value = {}
    if (!dialogOpen || providerId !== 'minimax' || !channelUid) return

    minimaxEndpointsLoading.value = true
    try {
      if (!await loadMiniMaxEndpoints(requestId, channelUid)) return
      // 后端用 TTL 控制实际请求频率；弹窗每次打开都回读一次最新缓存。
      const supported = minimaxEndpoints.value.filter(endpoint => endpoint.tokenPlanUsageSupported)
      if (supported.length > 0) {
        await Promise.allSettled(supported.map(endpoint => apiService.refreshEndpointTokenPlanUsage(endpoint.endpointUid)))
        if (requestId !== minimaxEndpointsRequestId) return
        await loadMiniMaxEndpoints(requestId, channelUid)
      }
    } catch (err) {
      if (requestId !== minimaxEndpointsRequestId) return
      minimaxEndpoints.value = []
      minimaxEndpointsError.value = err instanceof Error ? err.message : String(err)
    } finally {
      if (requestId === minimaxEndpointsRequestId) minimaxEndpointsLoading.value = false
    }
  },
  { immediate: true },
)

const refreshMinimaxUsage = async (endpoint: EndpointDetailItem) => {
  const requestId = minimaxEndpointsRequestId
  const endpointUid = endpoint.endpointUid
  minimaxUsageRefreshing.value = { ...minimaxUsageRefreshing.value, [endpointUid]: true }
  try {
    const response = await apiService.refreshEndpointTokenPlanUsage(endpointUid)
    if (requestId !== minimaxEndpointsRequestId) return
    const current = minimaxEndpoints.value.find(item => item.endpointUid === endpointUid)
    if (!current) return
    current.miniMaxTokenPlanUsage = response.usage
    current.miniMaxTokenPlanUsageError = ''
  } catch (err) {
    if (requestId !== minimaxEndpointsRequestId) return
    const current = minimaxEndpoints.value.find(item => item.endpointUid === endpointUid)
    if (!current) return
    current.miniMaxTokenPlanUsage = undefined
    current.miniMaxTokenPlanUsageError = err instanceof Error ? err.message : String(err)
  } finally {
    if (requestId === minimaxEndpointsRequestId) {
      minimaxUsageRefreshing.value = { ...minimaxUsageRefreshing.value, [endpointUid]: false }
    }
  }
}

const handleInput = () => {
  apiKeyError.value = ''
  duplicateKeyIndex.value = null
}

const handleAddKey = () => {
  const trimmed = newApiKey.value.trim()
  if (!trimmed) return

  // 检查重复
  const existingIndex = props.apiKeys.indexOf(trimmed)
  if (existingIndex !== -1) {
    apiKeyError.value = t('addChannel.duplicateKey')
    duplicateKeyIndex.value = existingIndex

    // 3秒后清除高亮
    setTimeout(() => {
      duplicateKeyIndex.value = null
    }, 3000)
    return
  }

  emit('update:apiKeys', [...props.apiKeys, trimmed])
  newApiKey.value = ''
  apiKeyError.value = ''
}

const removeKey = (index: number) => {
  const updated = props.apiKeys.filter((_, i) => i !== index)
  emit('update:apiKeys', updated)
}

// 活跃 Key 从表单移除（随保存生效）；已拉黑 Key 不在表单内，交由父组件立即调用后端删除。
const handleRemoveKey = (row: ChannelApiKeyRow) => {
  if (row.disabled) {
    emit('remove-key', row.key)
    return
  }
  removeKey(row.activeIndex)
}

onBeforeUnmount(() => {
  minimaxEndpointsRequestId++
  minimaxKeyHashRequestId++
})

const copyKey = (key: string) => {
  navigator.clipboard.writeText(key)
  copiedKey.value = key
  setTimeout(() => {
    copiedKey.value = ''
  }, 2000)
}

const getDisabledKeyLabel = (reason: string) => {
  const map: Record<string, string> = {
    'insufficient_balance': 'channelCard.blacklistReason.insufficient_balance',
    'insufficient_quota': 'channelCard.blacklistReason.insufficient_quota',
    'unauthorized': 'channelCard.blacklistReason.authentication_error',
    'invalid': 'channelCard.blacklistReason.invalid',
    'authentication_error': 'channelCard.blacklistReason.authentication_error',
    'permission_error': 'channelCard.blacklistReason.permission_error',
    // 组合级限制 (Key,模型) 的 reason：能力类差异统一展示"模型不可用"
    'model_not_found': 'channelCard.modelNotFound',
    'model_not_in_key_group': 'channelCard.modelNotFound',
    'image_generation_not_enabled': 'channelCard.modelNotFound',
  }
  return (map[reason] || 'channelCard.blacklistReason.unknown') as any
}
</script>

<style scoped>
.section-title {
  font-size: 1.125rem;
  font-weight: 600;
}

.cursor-pointer {
  cursor: pointer;
}

.key-sortable-list {
  min-height: 0;
}

.key-drag-handle {
  cursor: grab;
  opacity: 0.6;
  transition: opacity 0.15s ease;
}

.key-drag-handle:hover {
  opacity: 1;
}

.key-ghost {
  opacity: 0.5;
  background: rgba(var(--v-theme-primary), 0.08);
}

.animate-pulse {
  animation: pulse 1s ease-in-out 3;
}

@keyframes pulse {
  0%, 100% {
    opacity: 1;
  }
  50% {
    opacity: 0.6;
  }
}

/* 额度余量 5 档着色，阈值 5/10/20/30/50%，从深到浅表示余量递增。 */
.quota-critical {
  color: rgb(var(--v-theme-error)) !important;
  font-weight: 700;
}

.quota-danger {
  color: rgb(var(--v-theme-error)) !important;
}

.quota-warning {
  color: #EA580C !important;
}

.quota-caution {
  color: rgb(var(--v-theme-warning)) !important;
}

.quota-low {
  color: rgb(var(--v-theme-info)) !important;
}

.font-weight-mono {
  font-family: 'Monaco', 'Menlo', 'Ubuntu Mono', 'Courier New', monospace;
}

.volcengine-credential + .volcengine-credential {
  border-top: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
}

.volcengine-key-row {
  cursor: pointer;
}

.volcengine-key-detail {
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-top: 0;
  border-radius: 0 0 6px 6px;
}

.deepseek-credential + .deepseek-credential {
  border-top: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
}

.deepseek-balance-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(210px, 1fr));
  gap: 12px;
}

.deepseek-balance-currency {
  padding: 10px 12px;
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 6px;
}

.deepseek-balance-values {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.volcengine-key-fields {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 12px;
}

.compshare-plan-meta {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

@media (max-width: 700px) {
  .volcengine-key-fields {
    grid-template-columns: minmax(0, 1fr);
  }

  .compshare-plan-meta {
    grid-template-columns: minmax(0, 1fr);
  }

  .deepseek-balance-values {
    grid-template-columns: minmax(0, 1fr);
  }
}

</style>
