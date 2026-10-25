package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
)

type chatSettings struct {
	Enabled          bool    `json:"enabled"`
	ReplyProbability float64 `json:"reply_probability"`
	CooldownSeconds  int     `json:"cooldown_seconds"`
	StartHour        int     `json:"start_hour"`
	EndHour          int     `json:"end_hour"`
}

type modelSettings struct {
	Endpoint       string `json:"endpoint"`
	ChatModel      string `json:"chat_model"`
	VisionModel    string `json:"vision_model"`
	EmbeddingModel string `json:"embedding_model"`
	APIKey         string `json:"api_key,omitempty"`
	HasAPIKey      bool   `json:"has_api_key"`
	ClearAPIKey    bool   `json:"clear_api_key,omitempty"`
}

type learningSettings struct {
	Enabled              bool `json:"enabled"`
	Expressions          bool `json:"expressions"`
	Jargon               bool `json:"jargon"`
	DerivedRetentionDays int  `json:"derived_retention_days"`
	CrossGroup           bool `json:"cross_group"`
}

type memorySettings struct {
	Enabled              bool `json:"enabled"`
	TopK                 int  `json:"top_k"`
	RawRetentionDays     int  `json:"raw_retention_days"`
	DerivedRetentionDays int  `json:"derived_retention_days"`
	CrossGroup           bool `json:"cross_group"`
}

func defaultMaiBotSettings(section string) any {
	switch section {
	case "chat":
		return &chatSettings{ReplyProbability: 0.1, CooldownSeconds: 30, EndHour: 24}
	case "models":
		return &modelSettings{}
	case "learning":
		return &learningSettings{DerivedRetentionDays: 30}
	case "memory":
		return &memorySettings{TopK: 5, RawRetentionDays: 7, DerivedRetentionDays: 30}
	default:
		return nil
	}
}

func parseMaiBotSettings(section string, raw []byte) (any, error) {
	value := defaultMaiBotSettings(section)
	if value == nil {
		return nil, fail(404, "maibot_section_not_found")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return nil, fail(400, "invalid_maibot_settings")
	}
	valid := false
	switch v := value.(type) {
	case *chatSettings:
		valid = v.ReplyProbability >= 0 && v.ReplyProbability <= 1 && v.CooldownSeconds >= 5 &&
			v.CooldownSeconds <= 3600 && v.StartHour >= 0 && v.StartHour < v.EndHour && v.EndHour <= 24
	case *modelSettings:
		u, err := url.Parse(v.Endpoint)
		valid = err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil &&
			u.RawQuery == "" && u.Fragment == "" && len(v.Endpoint) <= 500 &&
			strings.TrimSpace(v.ChatModel) != "" && len(v.ChatModel) <= 200 &&
			len(v.VisionModel) <= 200 && len(v.EmbeddingModel) <= 200 && len(v.APIKey) <= 4096 &&
			!strings.ContainsAny(v.APIKey, "\r\n")
	case *learningSettings:
		valid = !v.CrossGroup && v.DerivedRetentionDays >= 1 && v.DerivedRetentionDays <= 90
	case *memorySettings:
		valid = !v.CrossGroup && v.RawRetentionDays == 7 && v.TopK >= 1 && v.TopK <= 20 &&
			v.DerivedRetentionDays >= 1 && v.DerivedRetentionDays <= 90
	}
	if !valid {
		return nil, fail(400, "invalid_maibot_settings")
	}
	return value, nil
}

func redactMaiBotSettings(value any) {
	if v, ok := value.(*modelSettings); ok {
		v.HasAPIKey = v.APIKey != ""
		v.APIKey = ""
		v.ClearAPIKey = false
	}
}

func (s *Server) maiBotSettings(w http.ResponseWriter, r *http.Request, p Principal) error {
	section, account := r.PathValue("section"), r.PathValue("id")
	value := defaultMaiBotSettings(section)
	if value == nil {
		return fail(404, "maibot_section_not_found")
	}
	var sealed []byte
	var version int64
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT sealed,version FROM maibot_settings
        WHERE account_id=$1 AND section=$2`, account, section).Scan(&sealed, &version)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if err == nil {
		plain, err := s.Vault.Open("maibot:"+account+":"+section, sealed)
		if err != nil {
			return fail(503, "maibot_settings_unreadable")
		}
		defer clear(plain)
		if err := json.Unmarshal(plain, value); err != nil {
			return fail(503, "maibot_settings_unreadable")
		}
	}
	redactMaiBotSettings(value)
	send(w, 200, map[string]any{"account_id": account, "section": section, "version": version,
		"config": value, "applied": false, "runtime_ready": false})
	return nil
}

func (s *Server) saveMaiBotSettings(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Version int64           `json:"version"`
		Config  json.RawMessage `json:"config"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Version < 0 {
		return fail(400, "invalid_maibot_settings")
	}
	section, account := r.PathValue("section"), r.PathValue("id")
	value, err := parseMaiBotSettings(section, in.Config)
	if err != nil {
		return err
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	// The account lock also serializes first inserts; a missing config row cannot be locked.
	if _, err = tx.Exec(r.Context(), `SELECT id FROM accounts WHERE id=$1 AND workspace='maibot' FOR UPDATE`, account); err != nil {
		return err
	}
	var version int64
	var previous []byte
	err = tx.QueryRow(r.Context(), `SELECT version,sealed FROM maibot_settings WHERE account_id=$1 AND section=$2`,
		account, section).Scan(&version, &previous)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if version != in.Version {
		return fail(409, "version_conflict")
	}
	if model, ok := value.(*modelSettings); ok {
		if model.APIKey == "" && !model.ClearAPIKey && version > 0 {
			plain, err := s.Vault.Open("maibot:"+account+":"+section, previous)
			if err != nil {
				return fail(503, "maibot_settings_unreadable")
			}
			var old modelSettings
			err = json.Unmarshal(plain, &old)
			clear(plain)
			if err != nil {
				return err
			}
			model.APIKey = old.APIKey
		}
		if model.ClearAPIKey {
			model.APIKey = ""
		}
		model.HasAPIKey, model.ClearAPIKey = false, false
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return err
	}
	defer clear(canonical)
	sealed, err := s.Vault.Seal("maibot:"+account+":"+section, canonical)
	if err != nil {
		return err
	}
	if err = tx.QueryRow(r.Context(), `INSERT INTO maibot_settings(account_id,section,sealed,updated_by)
        VALUES($1,$2,$3,$4) ON CONFLICT(account_id,section) DO UPDATE SET sealed=excluded.sealed,
        version=maibot_settings.version+1,updated_by=excluded.updated_by,updated_at=now() RETURNING version`,
		account, section, sealed, p.ID).Scan(&version); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE accounts SET epoch=epoch+1,version=version+1 WHERE id=$1`, account); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE operations SET status='cancelled',result_code='maibot_config_changed',updated_at=now()
        WHERE account_id=$1 AND status='queued'`, account); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, "", "maibot_settings_saved", account,
		map[string]any{"section": section, "version": version, "applied": false}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	redactMaiBotSettings(value)
	send(w, 200, map[string]any{"version": version, "config": value, "applied": false, "runtime_ready": false})
	return nil
}

func (s *Server) maiBotCapabilities(w http.ResponseWriter, r *http.Request, p Principal) error {
	items := []map[string]string{
		{"id": "chat", "name": "聊天配置", "status": "configuration_only", "scope": "account"},
		{"id": "models", "name": "模型服务", "status": "configuration_only", "scope": "account"},
		{"id": "personas", "name": "人设与回复风格", "status": "configuration_only", "scope": "account"},
		{"id": "learning", "name": "表达与黑话学习", "status": "configuration_only", "scope": "group_required"},
		{"id": "memory", "name": "长期记忆与向量检索", "status": "configuration_only", "scope": "group_required"},
		{"id": "people", "name": "人物与印象", "status": "engine_pending", "scope": "group_required"},
		{"id": "emoji", "name": "表情与媒体记忆", "status": "engine_pending", "scope": "group_required"},
		{"id": "statistics", "name": "用量与运行统计", "status": "engine_pending", "scope": "group_required"},
		{"id": "plugins", "name": "插件与工具", "status": "isolation_blocked", "scope": "superadmin"},
		{"id": "backups", "name": "备份与数据迁移", "status": "runbook_only", "scope": "superadmin"},
		{"id": "system", "name": "系统与升级", "status": "docker_only", "scope": "superadmin"},
	}
	send(w, 200, map[string]any{"items": items, "engine_ready": false, "limits": workspaceLimits(),
		"global_model_concurrency": 4, "per_group_concurrency": 1, "cross_group_memory": false,
		"upstream_commit": "062d45cf8bd4272ac57d7ec03fcf405e5a458211"})
	return nil
}

func (s *Server) maiBotEvents(w http.ResponseWriter, r *http.Request, p Principal) error {
	account := r.URL.Query().Get("account_id")
	if account != "" && !validUUID(account) {
		return fail(400, "invalid_resource_id")
	}
	return s.list(w, r, p, `SELECT id,action,resource_id,detail,created_at FROM audit_events
        WHERE action IN ('maibot_settings_saved') AND ($1='' OR resource_id=$1)
        ORDER BY id DESC LIMIT 100`, account)
}
