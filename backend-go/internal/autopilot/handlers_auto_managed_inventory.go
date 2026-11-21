package autopilot

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/utils"
)

type managedAccountCredentialView struct {
	CredentialUID             string                        `json:"credentialUid"`
	KeyMask                   string                        `json:"keyMask"`
	HasVolcengineAccessKey    bool                          `json:"hasVolcengineAccessKey,omitempty"`
	VolcengineAccessKeyIDMask string                        `json:"volcengineAccessKeyIdMask,omitempty"`
	VolcenginePlan            string                        `json:"volcenginePlan,omitempty"`
	VolcenginePlanTier        string                        `json:"volcenginePlanTier,omitempty"`
	VolcenginePlanStatus      string                        `json:"volcenginePlanStatus,omitempty"`
	VolcenginePlanUsage       *config.VolcenginePlanUsage   `json:"volcenginePlanUsage,omitempty"`
	VolcenginePlanBuckets     []config.VolcenginePlanBucket `json:"volcenginePlanBuckets,omitempty"`
	HasMiMoConsoleCookie      bool                          `json:"hasMiMoConsoleCookie,omitempty"`
	MiMoTokenPlan             *managedMiMoTokenPlanView     `json:"mimoTokenPlan,omitempty"`
	HasCompshareConsoleCookie bool                          `json:"hasCompshareConsoleCookie,omitempty"`
	CompsharePlan             *managedCompsharePlanView     `json:"compsharePlan,omitempty"`
	HasKimiConsoleToken       bool                          `json:"hasKimiConsoleToken,omitempty"`
	KimiCodeUsage             *config.KimiCodeUsageSnapshot `json:"kimiCodeUsage,omitempty"`
}

type managedMiMoTokenPlanView struct {
	PlanCode         string                         `json:"planCode"`
	PlanName         string                         `json:"planName"`
	CurrentPeriodEnd string                         `json:"currentPeriodEnd"`
	Expired          bool                           `json:"expired"`
	MonthUsage       config.MiMoTokenPlanUsageQuota `json:"monthUsage"`
	CurrentUsage     config.MiMoTokenPlanUsageQuota `json:"currentUsage"`
	ValidatedAt      time.Time                      `json:"validatedAt"`
}

type managedCompsharePlanView struct {
	PlanCode         string                          `json:"planCode"`
	PlanName         string                          `json:"planName"`
	DisplayName      string                          `json:"displayName"`
	Status           int                             `json:"status"`
	ConcurrencyLimit int64                           `json:"concurrencyLimit"`
	IsTeam           bool                            `json:"isTeam"`
	ExpireAt         int64                           `json:"expireAt"`
	FiveHourUsage    config.CompsharePlanUsageWindow `json:"fiveHourUsage"`
	WeeklyUsage      config.CompsharePlanUsageWindow `json:"weeklyUsage"`
	MonthlyUsage     config.CompsharePlanUsageWindow `json:"monthlyUsage"`
	ValidatedAt      time.Time                       `json:"validatedAt"`
}

type managedAccountChannelView struct {
	Kind                  string                                  `json:"kind"`
	ChannelUID            string                                  `json:"channelUid"`
	Name                  string                                  `json:"name"`
	ServiceType           string                                  `json:"serviceType"`
	Status                string                                  `json:"status"`
	ModelInventoryKnown   bool                                    `json:"modelInventoryKnown,omitempty"`
	DiscoveredModels      []string                                `json:"discoveredModels,omitempty"`
	ModelBindings         []managedAccountChannelModelBindingView `json:"modelBindings,omitempty"`
	ModelsUpdatedAt       *time.Time                              `json:"modelsUpdatedAt,omitempty"`
	ModelsDiscoveredAt    *time.Time                              `json:"modelsDiscoveredAt,omitempty"`
	ModelDiscoverySource  string                                  `json:"modelDiscoverySource,omitempty"`
	ModelDiscoveryMessage string                                  `json:"modelDiscoveryMessage,omitempty"`
	ProtocolAvailability  []managedProtocolAvailabilityView       `json:"protocolAvailability,omitempty"`
}

type managedProtocolAvailabilityView struct {
	Protocol              string                                  `json:"protocol"`
	ModelInventoryKnown   bool                                    `json:"modelInventoryKnown"`
	DiscoveredModels      []string                                `json:"discoveredModels,omitempty"`
	ModelBindings         []managedAccountChannelModelBindingView `json:"modelBindings,omitempty"`
	ModelsDiscoveredAt    *time.Time                              `json:"modelsDiscoveredAt,omitempty"`
	ModelDiscoverySource  string                                  `json:"modelDiscoverySource,omitempty"`
	ModelDiscoveryMessage string                                  `json:"modelDiscoveryMessage,omitempty"`
}

type managedAccountChannelModelBindingView struct {
	CredentialUID         string     `json:"credentialUid,omitempty"`
	KeyMask               string     `json:"keyMask"`
	Models                []string   `json:"models"`
	UpdatedAt             *time.Time `json:"updatedAt,omitempty"`
	ModelsDiscoveredAt    *time.Time `json:"modelsDiscoveredAt,omitempty"`
	ModelDiscoverySource  string     `json:"modelDiscoverySource,omitempty"`
	ModelDiscoveryMessage string     `json:"modelDiscoveryMessage,omitempty"`
}

type managedAccountView struct {
	AccountUID    string                         `json:"accountUid"`
	ProviderID    string                         `json:"providerId"`
	Name          string                         `json:"name"`
	Credentials   []managedAccountCredentialView `json:"credentials"`
	Channels      []managedAccountChannelView    `json:"channels"`
	EndpointCount int                            `json:"endpointCount"`
}

func handleListAccounts(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		cfg := deps.CfgManager.GetConfig()
		var profileStore *ProfileStore
		if deps.Runner != nil {
			profileStore = deps.Runner.store
		}
		listableAccounts := managedAccountsForList(cfg)
		accounts := make([]managedAccountView, 0, len(listableAccounts))
		for _, account := range listableAccounts {
			view := managedAccountView{AccountUID: account.AccountUID, ProviderID: account.ProviderID, Name: account.Name}
			for _, credential := range account.Credentials {
				credentialView := managedAccountCredentialView{
					CredentialUID: credential.CredentialUID,
					KeyMask:       utils.MaskAPIKey(credential.APIKey),
				}
				if credential.VolcengineAccessKey != nil {
					credentialView.HasVolcengineAccessKey = true
					credentialView.VolcengineAccessKeyIDMask = utils.MaskAPIKey(credential.VolcengineAccessKey.AccessKeyID)
					credentialView.VolcenginePlan = credential.VolcengineAccessKey.Plan
					credentialView.VolcenginePlanTier = credential.VolcengineAccessKey.PlanTier
					credentialView.VolcenginePlanStatus = credential.VolcengineAccessKey.PlanStatus
					credentialView.VolcenginePlanUsage = credential.VolcengineAccessKey.Usage
					credentialView.VolcenginePlanBuckets = credential.VolcengineAccessKey.Plans
				}
				if credential.MiMoConsole != nil {
					credentialView.HasMiMoConsoleCookie = true
					credentialView.MiMoTokenPlan = mimoTokenPlanView(credential.MiMoConsole)
				}
				if credential.CompshareConsole != nil {
					credentialView.HasCompshareConsoleCookie = true
					credentialView.CompsharePlan = compsharePlanView(credential.CompshareConsole)
				}
				if credential.KimiConsole != nil {
					credentialView.HasKimiConsoleToken = true
					usage := credential.KimiConsole.Usage
					credentialView.KimiCodeUsage = &usage
				}
				view.Credentials = append(view.Credentials, credentialView)
			}
			for _, channel := range deps.CfgManager.GetAccountChannels(account.AccountUID) {
				channelView := managedAccountChannelView{
					Kind: channel.Kind, ChannelUID: channel.Upstream.ChannelUID, Name: channel.Upstream.Name,
					ServiceType: channel.Upstream.ServiceType, Status: channel.Upstream.Status,
				}
				if profileStore != nil {
					profiles := profileStore.ListActiveByChannel(channel.Upstream.ChannelUID)
					inventory := managedChannelModelAvailabilityDetails(profiles)
					channelView.DiscoveredModels = inventory.models
					channelView.ModelBindings = inventory.bindings
					// modelsUpdatedAt 兼容旧前端，但语义必须是模型清单发现时间，
					// 不能再暴露会被 L1 刷新的 endpoint UpdatedAt。
					channelView.ModelsUpdatedAt = inventory.latestDiscoveredAtPointer()
					channelView.ModelsDiscoveredAt = inventory.latestDiscoveredAtPointer()
					channelView.ModelDiscoverySource = inventory.source
					channelView.ModelDiscoveryMessage = inventory.message
					channelView.ModelInventoryKnown = inventory.known
					channelView.ProtocolAvailability = managedProtocolAvailabilityViews(profiles)
				}
				view.Channels = append(view.Channels, channelView)
			}
			if profileStore != nil {
				view.EndpointCount = len(profileStore.ListByAccount(account.AccountUID))
			}
			accounts = append(accounts, view)
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"accounts": accounts})
		return nil
	}
}

// managedAccountsForList 将自定义自动托管渠道补入账号列表。
// 自定义渠道的 ProviderID 为空，配置层不会将其写入 ManagedAccounts；
// 但编辑器仍需要通过账号接口读取 endpoint profile 中的协议发现结果。
func managedAccountsForList(cfg config.Config) []config.ManagedAccountConfig {
	accounts := append([]config.ManagedAccountConfig(nil), cfg.ManagedAccounts...)
	accountIndex := make(map[string]int, len(accounts))
	credentialSeen := make(map[string]map[string]bool, len(accounts))
	deriveCredentials := make(map[string]bool, len(accounts))
	for i := range accounts {
		account := &accounts[i]
		accountIndex[account.AccountUID] = i
		deriveCredentials[account.AccountUID] = account.ProviderID == ""
		seen := make(map[string]bool, len(account.Credentials))
		for _, credential := range account.Credentials {
			seen[credential.CredentialUID] = true
		}
		credentialSeen[account.AccountUID] = seen
	}

	visit := func(kind string, channels []config.UpstreamConfig) {
		for i := range channels {
			channel := &channels[i]
			if !channel.AutoManaged || channel.AccountUID == "" {
				continue
			}
			index, exists := accountIndex[channel.AccountUID]
			if !exists {
				name := strings.TrimSuffix(channel.Name, accountRouteSuffix(kind))
				accounts = append(accounts, config.ManagedAccountConfig{
					AccountUID: channel.AccountUID,
					ProviderID: channel.ProviderID,
					Name:       name,
				})
				index = len(accounts) - 1
				accountIndex[channel.AccountUID] = index
				credentialSeen[channel.AccountUID] = make(map[string]bool)
				deriveCredentials[channel.AccountUID] = true
			}
			if !deriveCredentials[channel.AccountUID] {
				continue
			}

			existingByUID := make(map[string]config.ManagedAccountCredential, len(accounts[index].Credentials))
			for _, credential := range accounts[index].Credentials {
				existingByUID[credential.CredentialUID] = credential
			}

			addCredential := func(apiKey string, keyConfig *config.APIKeyConfig) {
				credentialUID := ""
				if keyConfig != nil {
					credentialUID = keyConfig.CredentialUID
				}
				if credentialUID == "" {
					credentialUID = channel.CredentialUIDForKey(apiKey)
				}
				if credentialUID == "" {
					return
				}
				seen := credentialSeen[channel.AccountUID]
				if seen[credentialUID] {
					return
				}
				credential := existingByUID[credentialUID]
				credential.CredentialUID = credentialUID
				if apiKey != "" {
					credential.APIKey = apiKey
				}
				accounts[index].Credentials = append(accounts[index].Credentials, credential)
				seen[credentialUID] = true
			}
			for _, apiKey := range channel.APIKeys {
				addCredential(apiKey, nil)
			}
			for j := range channel.APIKeyConfigs {
				if channel.APIKeyConfigs[j].Key == "" && channel.APIKeyConfigs[j].CredentialUID != "" {
					addCredential("", &channel.APIKeyConfigs[j])
				}
			}
			for _, disabled := range channel.DisabledAPIKeys {
				if disabled.Key != "" {
					addCredential(disabled.Key, disabled.Config)
				}
			}
		}
	}
	visit("messages", cfg.Upstream)
	visit("chat", cfg.ChatUpstream)
	visit("responses", cfg.ResponsesUpstream)
	visit("gemini", cfg.GeminiUpstream)
	visit("images", cfg.ImagesUpstream)
	visit("vectors", cfg.VectorsUpstream)
	return accounts
}

type managedChannelModelInventory struct {
	models             []string
	bindings           []managedAccountChannelModelBindingView
	latestUpdatedAt    time.Time
	latestDiscoveredAt time.Time
	source             string
	message            string
	known              bool
}

func (inventory managedChannelModelInventory) latestUpdatedAtPointer() *time.Time {
	if inventory.latestUpdatedAt.IsZero() {
		return nil
	}
	value := inventory.latestUpdatedAt
	return &value
}

func (inventory managedChannelModelInventory) latestDiscoveredAtPointer() *time.Time {
	if inventory.latestDiscoveredAt.IsZero() {
		return nil
	}
	value := inventory.latestDiscoveredAt
	return &value
}

func managedChannelModelAvailabilityDetails(profiles []*KeyEndpointProfile) managedChannelModelInventory {
	return managedModelAvailabilityDetails(profiles, func(profile *KeyEndpointProfile) managedProfileModelInventory {
		return managedProfileModelInventory{
			models:       profile.AvailableModels,
			known:        profileHasModelInventory(profile),
			discoveredAt: profile.ModelsDiscoveredAt,
			source:       profile.ModelDiscoverySource,
			message:      profile.ModelDiscoveryMessage,
		}
	})
}

type managedProfileModelInventory struct {
	models       []string
	known        bool
	discoveredAt *time.Time
	source       string
	message      string
}

func managedModelAvailabilityDetails(
	profiles []*KeyEndpointProfile,
	selectInventory func(*KeyEndpointProfile) managedProfileModelInventory,
) managedChannelModelInventory {
	type bindingAggregate struct {
		credentialUID    string
		keyMask          string
		models           map[string]struct{}
		updatedAt        time.Time
		discoveredAt     time.Time
		discoverySource  string
		discoveryMessage string
	}

	allModels := make(map[string]struct{})
	bindings := make(map[string]*bindingAggregate)
	var latestUpdatedAt time.Time
	var latestDiscoveredAt time.Time
	sources := make(map[string]struct{})
	var latestMessage string
	var latestMessageAt time.Time
	known := false
	for _, profile := range profiles {
		if profile == nil {
			continue
		}
		profileInventory := selectInventory(profile)
		if !profileInventory.known {
			continue
		}
		known = true
		bindingKey := strings.TrimSpace(profile.CredentialUID)
		if bindingKey == "" {
			bindingKey = strings.TrimSpace(profile.KeyHash)
		}
		if bindingKey == "" {
			bindingKey = profile.EndpointUID
		}
		binding := bindings[bindingKey]
		if binding == nil {
			binding = &bindingAggregate{
				credentialUID: profile.CredentialUID,
				keyMask:       profile.KeyMask,
				models:        make(map[string]struct{}),
			}
			bindings[bindingKey] = binding
		}
		if binding.keyMask == "" {
			binding.keyMask = profile.KeyMask
		}
		for _, model := range profileInventory.models {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			binding.models[model] = struct{}{}
			allModels[model] = struct{}{}
		}
		if profile.UpdatedAt.After(binding.updatedAt) {
			binding.updatedAt = profile.UpdatedAt
		}
		if profile.UpdatedAt.After(latestUpdatedAt) {
			latestUpdatedAt = profile.UpdatedAt
		}
		if profileInventory.source != "" {
			sources[profileInventory.source] = struct{}{}
		}
		if profileInventory.discoveredAt != nil {
			discoveredAt := profileInventory.discoveredAt.UTC()
			if discoveredAt.After(binding.discoveredAt) {
				binding.discoveredAt = discoveredAt
				binding.discoverySource = profileInventory.source
				binding.discoveryMessage = profileInventory.message
			}
			if discoveredAt.After(latestDiscoveredAt) {
				latestDiscoveredAt = discoveredAt
			}
			if discoveredAt.After(latestMessageAt) && strings.TrimSpace(profileInventory.message) != "" {
				latestMessageAt = discoveredAt
				latestMessage = profileInventory.message
			}
		}
	}

	modelList := sortedModelSet(allModels)
	bindingList := make([]managedAccountChannelModelBindingView, 0, len(bindings))
	for _, binding := range bindings {
		view := managedAccountChannelModelBindingView{
			CredentialUID:         binding.credentialUID,
			KeyMask:               binding.keyMask,
			Models:                sortedModelSet(binding.models),
			ModelDiscoverySource:  binding.discoverySource,
			ModelDiscoveryMessage: binding.discoveryMessage,
		}
		if !binding.updatedAt.IsZero() {
			updatedAt := binding.updatedAt
			view.UpdatedAt = &updatedAt
		}
		if !binding.discoveredAt.IsZero() {
			discoveredAt := binding.discoveredAt
			view.ModelsDiscoveredAt = &discoveredAt
		}
		bindingList = append(bindingList, view)
	}
	sort.Slice(bindingList, func(i, j int) bool {
		if bindingList[i].KeyMask == bindingList[j].KeyMask {
			return bindingList[i].CredentialUID < bindingList[j].CredentialUID
		}
		return bindingList[i].KeyMask < bindingList[j].KeyMask
	})

	source := ""
	if len(sources) == 1 {
		for value := range sources {
			source = value
		}
	} else if len(sources) > 1 {
		source = "mixed"
	}
	return managedChannelModelInventory{
		models:             modelList,
		bindings:           bindingList,
		latestUpdatedAt:    latestUpdatedAt,
		latestDiscoveredAt: latestDiscoveredAt,
		source:             source,
		message:            latestMessage,
		known:              known,
	}
}

func managedProtocolAvailabilityViews(profiles []*KeyEndpointProfile) []managedProtocolAvailabilityView {
	views := make([]managedProtocolAvailabilityView, 0, len(discoverableProtocols))
	for _, protocol := range discoverableProtocols {
		inventory := managedModelAvailabilityDetails(profiles, func(profile *KeyEndpointProfile) managedProfileModelInventory {
			return managedProtocolProfileInventory(profile, protocol)
		})
		if !inventory.known {
			continue
		}
		views = append(views, managedProtocolAvailabilityView{
			Protocol:              protocol,
			ModelInventoryKnown:   true,
			DiscoveredModels:      inventory.models,
			ModelBindings:         inventory.bindings,
			ModelsDiscoveredAt:    inventory.latestDiscoveredAtPointer(),
			ModelDiscoverySource:  inventory.source,
			ModelDiscoveryMessage: inventory.message,
		})
	}
	return views
}

func managedProtocolProfileInventory(profile *KeyEndpointProfile, protocol string) managedProfileModelInventory {
	if profile == nil {
		return managedProfileModelInventory{}
	}
	models, known := profile.ProtocolModels[protocol]
	if !known && protocolForServiceType(profile.ServiceType) == protocol && profileHasModelInventory(profile) {
		models = profile.AvailableModels
		known = true
	}
	discoveredAt, hasDiscoveredAt := profile.ProtocolDiscoveredAt[protocol]
	var discoveredAtPointer *time.Time
	if hasDiscoveredAt {
		value := discoveredAt.UTC()
		discoveredAtPointer = &value
	} else if known {
		discoveredAtPointer = profile.ModelsDiscoveredAt
	}
	source := profile.ProtocolDiscoverySource[protocol]
	if source == "" && known {
		source = profile.ModelDiscoverySource
	}
	message := profile.ProtocolDiscoveryMessage[protocol]
	if message == "" && known {
		message = profile.ModelDiscoveryMessage
	}
	return managedProfileModelInventory{
		models:       models,
		known:        known,
		discoveredAt: discoveredAtPointer,
		source:       source,
		message:      message,
	}
}

func managedChannelModelAvailability(profiles []*KeyEndpointProfile) ([]string, []managedAccountChannelModelBindingView, *time.Time, bool) {
	inventory := managedChannelModelAvailabilityDetails(profiles)
	return inventory.models, inventory.bindings, inventory.latestUpdatedAtPointer(), inventory.known
}

// profileHasModelInventory 区分尚未获得模型清单与成功发现到空清单。
// 自动发现将 HTTP 200 的空 models 响应持久化为 Source=auto_discovery；它仍是权威结果，
// 因此需要展示为当前 Key 的 0 个模型，而不是回退到可能过期的配置白名单。
func profileHasModelInventory(profile *KeyEndpointProfile) bool {
	return len(profile.AvailableModels) > 0 ||
		strings.EqualFold(strings.TrimSpace(profile.Source), "auto_discovery") ||
		strings.TrimSpace(profile.ModelDiscoverySource) != ""
}

func sortedModelSet(models map[string]struct{}) []string {
	if len(models) == 0 {
		return nil
	}
	result := make([]string, 0, len(models))
	for model := range models {
		result = append(result, model)
	}
	sort.Strings(result)
	return result
}

func mimoTokenPlanView(source *config.MiMoConsoleCredential) *managedMiMoTokenPlanView {
	if source == nil {
		return nil
	}
	return &managedMiMoTokenPlanView{
		PlanCode: source.PlanCode, PlanName: source.PlanName, CurrentPeriodEnd: source.CurrentPeriodEnd,
		Expired: source.Expired, MonthUsage: source.MonthUsage, CurrentUsage: source.CurrentUsage, ValidatedAt: source.ValidatedAt,
	}
}

func compsharePlanView(source *config.CompshareConsoleCredential) *managedCompsharePlanView {
	if source == nil {
		return nil
	}
	return &managedCompsharePlanView{
		PlanCode: source.PlanCode, PlanName: source.PlanName, DisplayName: source.DisplayName,
		Status: source.Status, ConcurrencyLimit: source.ConcurrencyLimit, IsTeam: source.IsTeam,
		ExpireAt: source.ExpireAt, FiveHourUsage: source.FiveHourUsage, WeeklyUsage: source.WeeklyUsage,
		MonthlyUsage: source.MonthlyUsage, ValidatedAt: source.ValidatedAt,
	}
}
