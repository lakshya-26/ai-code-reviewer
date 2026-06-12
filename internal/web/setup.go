// Package web provides the HTTP handlers for the public-facing setup UI.
// Users land here after installing the GitHub App via the GitHub-controlled
// redirect (installation_id is injected by GitHub, so only the installer
// arrives on this page naturally).
package web

import (
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/ai-code-reviewer/ai-code-reviewer/internal/storage"
)

// Store is the subset of storage.Store that the web handlers need.
type Store interface {
	GetOrCreate(ctx context.Context, installationID int64, accountLogin string) (*storage.Installation, error)
	Get(ctx context.Context, installationID int64) (*storage.Installation, error)
	UpdateConfig(ctx context.Context, installationID int64, provider, apiKey, model string) error
}

// SetupHandler handles GET /setup and POST /setup.
// GitHub redirects here after installation: /setup?installation_id=xxx&setup_action=install
func SetupHandler(store Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleSetupGet(w, r, store)
		case http.MethodPost:
			handleSetupPost(w, r, store)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// ─── GET /setup ───────────────────────────────────────────────────────────────

type setupPageData struct {
	InstallationID int64
	AccountLogin   string
	Provider       string
	Model          string
	HasKey         bool
	FreeUsed       int
	FreeLimit      int
	UsagePct       int  // 0-100 for the progress bar
	AtLimit        bool
	Success        string
	Error          string
	Providers      []providerOption
}

type providerOption struct {
	Value string
	Label string
}

var supportedProviders = []providerOption{
	{"groq", "Groq — fast inference, free tier (recommended)"},
	{"openai", "OpenAI (GPT-4o, GPT-4.1…)"},
	{"claude", "Anthropic Claude"},
	{"gemini", "Google Gemini"},
	{"grok", "xAI Grok"},
}

func handleSetupGet(w http.ResponseWriter, r *http.Request, store Store) {
	installationID, err := parseInstallationID(r)
	if err != nil {
		http.Error(w, "missing or invalid installation_id", http.StatusBadRequest)
		return
	}

	inst, err := store.GetOrCreate(r.Context(), installationID, "")
	if err != nil {
		slog.Error("setup: get-or-create installation", "err", err, "installation_id", installationID)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	pct := 0
	if inst.FreeReviewsLimit > 0 {
		pct = inst.FreeReviewsUsed * 100 / inst.FreeReviewsLimit
		if pct > 100 {
			pct = 100
		}
	}

	data := setupPageData{
		InstallationID: installationID,
		AccountLogin:   inst.AccountLogin,
		Provider:       inst.Provider,
		Model:          inst.Model,
		HasKey:         inst.HasCustomKey(),
		FreeUsed:       inst.FreeReviewsUsed,
		FreeLimit:      inst.FreeReviewsLimit,
		UsagePct:       pct,
		AtLimit:        inst.IsOverFreeLimit(),
		Success:        r.URL.Query().Get("success"),
		Error:          r.URL.Query().Get("error"),
		Providers:      supportedProviders,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := setupTmpl.Execute(w, data); err != nil {
		slog.Error("setup: render template", "err", err)
	}
}

// ─── POST /setup ──────────────────────────────────────────────────────────────

func handleSetupPost(w http.ResponseWriter, r *http.Request, store Store) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	installationID, err := parseInstallationID(r)
	if err != nil {
		http.Error(w, "missing or invalid installation_id", http.StatusBadRequest)
		return
	}

	provider := strings.TrimSpace(r.FormValue("provider"))
	apiKey := strings.TrimSpace(r.FormValue("api_key"))
	model := strings.TrimSpace(r.FormValue("model"))
	action := r.FormValue("action")

	redirectBase := fmt.Sprintf("/setup?installation_id=%d", installationID)

	// "Remove key" action — clears the custom config and reverts to free tier.
	if action == "remove" {
		if err := store.UpdateConfig(r.Context(), installationID, "", "", ""); err != nil {
			slog.Error("setup: remove config", "err", err)
			http.Redirect(w, r, redirectBase+"&error=Failed+to+remove+configuration", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, redirectBase+"&success=Configuration+removed.+Using+free+tier.", http.StatusSeeOther)
		return
	}

	// Validate provider
	if !isValidProvider(provider) {
		http.Redirect(w, r, redirectBase+"&error=Invalid+provider+selected", http.StatusSeeOther)
		return
	}

	// API key is required when saving a new config (not removing).
	if apiKey == "" {
		http.Redirect(w, r, redirectBase+"&error=API+key+is+required", http.StatusSeeOther)
		return
	}

	if err := store.UpdateConfig(r.Context(), installationID, provider, apiKey, model); err != nil {
		slog.Error("setup: save config", "err", err, "installation_id", installationID)
		http.Redirect(w, r, redirectBase+"&error=Failed+to+save+configuration", http.StatusSeeOther)
		return
	}

	slog.Info("setup: configuration saved", "installation_id", installationID, "provider", provider)
	http.Redirect(w, r, redirectBase+"&success=Configuration+saved+successfully!", http.StatusSeeOther)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func parseInstallationID(r *http.Request) (int64, error) {
	raw := r.URL.Query().Get("installation_id")
	if raw == "" {
		raw = r.FormValue("installation_id")
	}
	if raw == "" {
		return 0, fmt.Errorf("missing installation_id")
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid installation_id: %w", err)
	}
	return id, nil
}

func isValidProvider(p string) bool {
	for _, opt := range supportedProviders {
		if opt.Value == p {
			return true
		}
	}
	return false
}

// ─── HTML template ────────────────────────────────────────────────────────────

var setupTmpl = template.Must(template.New("setup").Parse(setupHTML))

const setupHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>DiffSense AI — Setup</title>
<style>
  *{box-sizing:border-box;margin:0;padding:0}
  body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#0d1117;color:#e6edf3;min-height:100vh;display:flex;align-items:center;justify-content:center;padding:24px}
  .card{background:#161b22;border:1px solid #30363d;border-radius:12px;padding:40px;width:100%;max-width:520px}
  .logo{display:flex;align-items:center;gap:12px;margin-bottom:32px}
  .logo-icon{width:40px;height:40px;background:linear-gradient(135deg,#238636,#1a7f37);border-radius:10px;display:flex;align-items:center;justify-content:center;font-size:20px}
  .logo h1{font-size:20px;font-weight:700;color:#e6edf3}
  .logo p{font-size:13px;color:#8b949e;margin-top:2px}
  .usage-bar{background:#21262d;border:1px solid #30363d;border-radius:8px;padding:16px;margin-bottom:24px}
  .usage-header{display:flex;justify-content:space-between;align-items:center;margin-bottom:10px}
  .usage-label{font-size:13px;color:#8b949e}
  .usage-count{font-size:13px;font-weight:600;color:#e6edf3}
  .usage-track{height:6px;background:#30363d;border-radius:3px;overflow:hidden}
  .usage-fill{height:100%;border-radius:3px;background:linear-gradient(90deg,#238636,#2ea043);transition:width 0.3s}
  .usage-fill.warning{background:linear-gradient(90deg,#9e6a03,#d29922)}
  .usage-fill.danger{background:linear-gradient(90deg,#b62324,#da3633)}
  .badge{display:inline-flex;align-items:center;gap:6px;padding:4px 10px;border-radius:20px;font-size:12px;font-weight:600;margin-bottom:24px}
  .badge.has-key{background:#1f4f2b;color:#3fb950;border:1px solid #238636}
  .badge.free-tier{background:#1f2d3b;color:#58a6ff;border:1px solid #1f6feb}
  .badge.at-limit{background:#3d1c1c;color:#f85149;border:1px solid #da3633}
  .section-title{font-size:14px;font-weight:600;color:#e6edf3;margin-bottom:12px}
  .form-group{margin-bottom:16px}
  label{display:block;font-size:13px;color:#8b949e;margin-bottom:6px}
  select,input[type=text],input[type=password]{width:100%;background:#0d1117;border:1px solid #30363d;border-radius:6px;color:#e6edf3;padding:10px 12px;font-size:14px;outline:none;transition:border-color 0.2s}
  select:focus,input:focus{border-color:#58a6ff}
  select option{background:#161b22}
  .hint{font-size:12px;color:#6e7681;margin-top:6px}
  .btn{width:100%;padding:11px;border-radius:6px;font-size:14px;font-weight:600;cursor:pointer;border:none;transition:opacity 0.2s}
  .btn-primary{background:#238636;color:#fff}
  .btn-primary:hover{opacity:0.9}
  .btn-danger{background:#transparent;color:#f85149;border:1px solid #da3633;margin-top:10px}
  .btn-danger:hover{background:#3d1c1c}
  .alert{padding:12px 16px;border-radius:6px;font-size:13px;margin-bottom:20px}
  .alert.success{background:#1f4f2b;border:1px solid #238636;color:#3fb950}
  .alert.error{background:#3d1c1c;border:1px solid #da3633;color:#f85149}
  .divider{border:none;border-top:1px solid #30363d;margin:24px 0}
  .footer{text-align:center;margin-top:24px;font-size:12px;color:#6e7681}
  a{color:#58a6ff;text-decoration:none}
</style>
</head>
<body>
<div class="card">
  <div class="logo">
    <div class="logo-icon">🔍</div>
    <div>
      <h1>DiffSense AI</h1>
      <p>GitHub App Configuration</p>
    </div>
  </div>

  {{if .Success}}<div class="alert success">✓ {{.Success}}</div>{{end}}
  {{if .Error}}<div class="alert error">✗ {{.Error}}</div>{{end}}

  <!-- Usage bar -->
  <div class="usage-bar">
    <div class="usage-header">
      <span class="usage-label">Free reviews used</span>
      <span class="usage-count">{{.FreeUsed}} / {{.FreeLimit}}</span>
    </div>
    <div class="usage-track">
      <div class="usage-fill {{if .AtLimit}}danger{{else if gt .UsagePct 50}}warning{{end}}"
           style="width:{{.UsagePct}}%"></div>
    </div>
  </div>

  {{if .HasKey}}
    <div class="badge has-key">✓ Using your own API key ({{.Provider}})</div>
  {{else if .AtLimit}}
    <div class="badge at-limit">⚠ Free tier limit reached — add your API key below</div>
  {{else}}
    <div class="badge free-tier">● Using shared free tier ({{.FreeLimit}} reviews/installation)</div>
  {{end}}

  <!-- Config form -->
  <p class="section-title">API Key Configuration</p>
  <form method="POST" action="/setup?installation_id={{.InstallationID}}">
    <input type="hidden" name="installation_id" value="{{.InstallationID}}">

    <div class="form-group">
      <label for="provider">AI Provider</label>
      <select name="provider" id="provider">
        {{range .Providers}}
          <option value="{{.Value}}" {{if eq $.Provider .Value}}selected{{end}}>{{.Label}}</option>
        {{end}}
      </select>
    </div>

    <div class="form-group">
      <label for="api_key">API Key</label>
      <input type="password" name="api_key" id="api_key"
             placeholder="{{if .HasKey}}••••••••••••••••••••••••{{else}}Enter your API key{{end}}"
             autocomplete="off">
      <p class="hint">Leave blank to keep your current key unchanged.</p>
    </div>

    <div class="form-group">
      <label for="model">Model <span style="color:#6e7681">(optional)</span></label>
      <input type="text" name="model" id="model"
             value="{{.Model}}"
             placeholder="e.g. gpt-4o, claude-sonnet-4-5, llama-3.3-70b-versatile">
      <p class="hint">Leave blank to use the provider's recommended default.</p>
    </div>

    <button type="submit" class="btn btn-primary">Save Configuration</button>
  </form>

  {{if .HasKey}}
  <hr class="divider">
  <form method="POST" action="/setup?installation_id={{.InstallationID}}">
    <input type="hidden" name="installation_id" value="{{.InstallationID}}">
    <input type="hidden" name="action" value="remove">
    <button type="submit" class="btn btn-danger">Remove API key (revert to free tier)</button>
  </form>
  {{end}}

  <div class="footer">
    Installation #{{.InstallationID}}
    {{if .AccountLogin}} · <strong>{{.AccountLogin}}</strong>{{end}}
    · <a href="https://github.com/apps/diffsense-ai" target="_blank">App Settings</a>
  </div>
</div>
</body>
</html>
`
