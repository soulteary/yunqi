package autopilot

import (
	"log"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// ── 请求/响应类型 ──

// CreateTaskTemplateRequest POST /api/task-templates 请求体。
type CreateTaskTemplateRequest struct {
	Name        string     `json:"name" binding:"required"`
	Description string     `json:"description,omitempty"`
	OutputMode  OutputMode `json:"outputMode" binding:"required"`

	MatchTaskClasses []string `json:"matchTaskClasses,omitempty"`
	MatchDomains     []string `json:"matchDomains,omitempty"`

	PromptTemplate string `json:"promptTemplate" binding:"required"`
	Priority       int    `json:"priority,omitempty"`
	Enabled        *bool  `json:"enabled,omitempty"` // 指针区分零值和未传
}

// UpdateTaskTemplateRequest PUT /api/task-templates/:id 请求体。
type UpdateTaskTemplateRequest struct {
	Name        *string     `json:"name,omitempty"`
	Description *string     `json:"description,omitempty"`
	OutputMode  *OutputMode `json:"outputMode,omitempty"`

	MatchTaskClasses []string `json:"matchTaskClasses,omitempty"`
	MatchDomains     []string `json:"matchDomains,omitempty"`

	PromptTemplate *string `json:"promptTemplate,omitempty"`
	Priority       *int    `json:"priority,omitempty"`
	Enabled        *bool   `json:"enabled,omitempty"`
}

// TaskTemplateResponse 单条模板响应。
type TaskTemplateResponse struct {
	TemplateID  string     `json:"templateId"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	OutputMode  OutputMode `json:"outputMode"`

	MatchTaskClasses []string `json:"matchTaskClasses,omitempty"`
	MatchDomains     []string `json:"matchDomains,omitempty"`

	PromptTemplate string `json:"promptTemplate"`
	Priority       int    `json:"priority"`
	Enabled        bool   `json:"enabled"`

	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// TaskTemplatesResponse GET /api/task-templates 列表响应。
type TaskTemplatesResponse struct {
	Templates []TaskTemplateResponse `json:"templates"`
	Total     int                    `json:"total"`
}

// ── 路由注册 ──

// RegisterTaskTemplateRoutes 注册本地任务模板 CRUD API 到给定路由组。
//
// 路由：
//
//	GET    /api/task-templates           — 列表
//	POST   /api/task-templates           — 创建
//	GET    /api/task-templates/:id       — 详情
//	PUT    /api/task-templates/:id       — 更新
//	DELETE /api/task-templates/:id       — 删除
func RegisterTaskTemplateRoutes(router fiber.Router, store *LocalTaskTemplateStore) {
	group := router.Group("/task-templates")
	{
		group.Get("", handleListTaskTemplates(store))
		group.Post("", handleCreateTaskTemplate(store))
		group.Get("/:id", handleGetTaskTemplate(store))
		group.Put("/:id", handleUpdateTaskTemplate(store))
		group.Delete("/:id", handleDeleteTaskTemplate(store))
	}
}

// PLACEHOLDER_HANDLERS

// handleListTaskTemplates GET /api/task-templates
func handleListTaskTemplates(store *LocalTaskTemplateStore) fiber.Handler {
	return func(c fiber.Ctx) error {
		templates := store.ListAll()
		items := make([]TaskTemplateResponse, 0, len(templates))
		for _, t := range templates {
			items = append(items, toTaskTemplateResponse(t))
		}
		httpbridge.JSON(c, http.StatusOK, TaskTemplatesResponse{
			Templates: items,
			Total:     len(items),
		})
		return nil
	}
}

// handleCreateTaskTemplate POST /api/task-templates
func handleCreateTaskTemplate(store *LocalTaskTemplateStore) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req CreateTaskTemplateRequest
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "请求参数无效: " + err.Error()})
			return nil
		}

		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}

		t := &LocalTaskTemplate{
			TemplateID:       GenerateTemplateID(),
			Name:             req.Name,
			Description:      req.Description,
			OutputMode:       req.OutputMode,
			MatchTaskClasses: normalizeStringSlice(req.MatchTaskClasses),
			MatchDomains:     normalizeStringSlice(req.MatchDomains),
			PromptTemplate:   req.PromptTemplate,
			Priority:         req.Priority,
			Enabled:          enabled,
		}

		if err := t.Validate(); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "校验失败: " + err.Error()})
			return nil
		}

		if err := store.Upsert(t); err != nil {
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": "创建失败: " + err.Error()})
			return nil
		}

		log.Printf("[TaskTemplate-Create] 创建模板: id=%s name=%s", t.TemplateID, t.Name)
		httpbridge.JSON(c, http.StatusCreated, toTaskTemplateResponse(t))
		return nil
	}
}

// handleGetTaskTemplate GET /api/task-templates/:id
func handleGetTaskTemplate(store *LocalTaskTemplateStore) fiber.Handler {
	return func(c fiber.Ctx) error {
		id := httpbridge.Param(c, "id")
		if id == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "id 不能为空"})
			return nil
		}

		t := store.Get(id)
		if t == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "模板不存在"})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, toTaskTemplateResponse(t))
		return nil
	}
}

// PLACEHOLDER_UPDATE_DELETE

// handleUpdateTaskTemplate PUT /api/task-templates/:id
func handleUpdateTaskTemplate(store *LocalTaskTemplateStore) fiber.Handler {
	return func(c fiber.Ctx) error {
		id := httpbridge.Param(c, "id")
		if id == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "id 不能为空"})
			return nil
		}

		existing := store.Get(id)
		if existing == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "模板不存在"})
			return nil
		}

		var req UpdateTaskTemplateRequest
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "请求参数无效: " + err.Error()})
			return nil
		}

		// 原地修改现有模板
		if req.Name != nil {
			existing.Name = *req.Name
		}
		if req.Description != nil {
			existing.Description = *req.Description
		}
		if req.OutputMode != nil {
			existing.OutputMode = *req.OutputMode
		}
		if req.MatchTaskClasses != nil {
			existing.MatchTaskClasses = normalizeStringSlice(req.MatchTaskClasses)
		}
		if req.MatchDomains != nil {
			existing.MatchDomains = normalizeStringSlice(req.MatchDomains)
		}
		if req.PromptTemplate != nil {
			existing.PromptTemplate = *req.PromptTemplate
		}
		if req.Priority != nil {
			existing.Priority = *req.Priority
		}
		if req.Enabled != nil {
			existing.Enabled = *req.Enabled
		}

		if err := existing.Validate(); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "校验失败: " + err.Error()})
			return nil
		}

		if err := store.Upsert(existing); err != nil {
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": "更新失败: " + err.Error()})
			return nil
		}

		log.Printf("[TaskTemplate-Update] 更新模板: id=%s name=%s", existing.TemplateID, existing.Name)
		httpbridge.JSON(c, http.StatusOK, toTaskTemplateResponse(existing))
		return nil
	}
}

// handleDeleteTaskTemplate DELETE /api/task-templates/:id
func handleDeleteTaskTemplate(store *LocalTaskTemplateStore) fiber.Handler {
	return func(c fiber.Ctx) error {
		id := httpbridge.Param(c, "id")
		if id == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "id 不能为空"})
			return nil
		}

		if store.Get(id) == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "模板不存在"})
			return nil
		}

		if err := store.Delete(id); err != nil {
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": "删除失败: " + err.Error()})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"message": "已删除", "templateId": id})
		return nil
	}
}

// ── 内部辅助 ──

// toTaskTemplateResponse 将内部模板转为 API 响应。
func toTaskTemplateResponse(t *LocalTaskTemplate) TaskTemplateResponse {
	r := TaskTemplateResponse{
		TemplateID:       t.TemplateID,
		Name:             t.Name,
		Description:      t.Description,
		OutputMode:       t.OutputMode,
		MatchTaskClasses: copyStrings(t.MatchTaskClasses),
		MatchDomains:     copyStrings(t.MatchDomains),
		PromptTemplate:   t.PromptTemplate,
		Priority:         t.Priority,
		Enabled:          t.Enabled,
		CreatedAt:        t.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:        t.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
	return r
}

// normalizeStringSlice 标准化字符串 slice：去除空白项、去重、统一小写。
// 输入 nil 返回 nil。
func normalizeStringSlice(items []string) []string {
	if items == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.ToLower(strings.TrimSpace(item))
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// PLACEHOLDER_CONTEXT_HELPER

// ContextKeyTaskTemplateStore 是 fiber.Ctx 中 LocalTaskTemplateStore 的 key。
// 需与 compact_local.go 中的常量保持一致。
const ContextKeyTaskTemplateStore = "autopilot_task_template_store"

// SetTaskTemplateStore 在 fiber.Ctx 中设置模板存储（供 compact 层读取）。
func SetTaskTemplateStore(c fiber.Ctx, store *LocalTaskTemplateStore) {
	httpbridge.SetValue(c, ContextKeyTaskTemplateStore, store)
}

// GetTaskTemplateStoreFromContext 从 fiber.Ctx 获取模板存储。
// 不存在时返回 nil（零模板模式，使用默认提示词）。
func GetTaskTemplateStoreFromContext(c fiber.Ctx) *LocalTaskTemplateStore {
	val, ok := httpbridge.Value(c, ContextKeyTaskTemplateStore)
	if !ok {
		return nil
	}
	store, _ := val.(*LocalTaskTemplateStore)
	return store
}
