package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/provider"
)

// mothxHome is a sandbox home with a provider serving deepseek/pro, so
// magpie has a model to wire into MothX.
func mothxHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("MOTHX_DIR", "")
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro"}}); err != nil {
		t.Fatal(err)
	}
	return home
}

// MothX reads its providers and startup model from ~/.mothx/settings.json:
// magpie goes in as a provider of its own with magpie's models, and the
// session model (defaultProvider/defaultModel) points at one; the user's own
// provider and settings stay, and going back to one of MothX's own takes
// magpie out.
func TestMothX(t *testing.T) {
	home := mothxHome(t)
	dir := filepath.Join(home, ".mothx")
	settings := filepath.Join(dir, "settings.json")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(settings, []byte(`{
  "defaultProvider": "openai",
  "defaultModel": "gpt-5",
  "providers": {"openai": {"baseUrl": "https://api.openai.com/v1", "apiKey": "sk-mine", "api": "openai-chat", "models": [{"id": "gpt-5", "name": "GPT-5"}]}},
  "theme": "dark"
}`), 0o600)

	var a *Agent
	for _, x := range All() {
		if x.ID == "mothx" {
			a = x
		}
	}
	if a == nil {
		t.Fatal("no mothx agent")
	}
	if !a.Detected() {
		t.Fatal("not detected")
	}
	f := a.Field("model")
	if got := f.Get(); got != "openai/gpt-5" {
		t.Fatalf("get: %q", got)
	}

	if err := f.Set("magpie/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, settings)
	if m["defaultProvider"] != "magpie" || m["defaultModel"] != "deepseek/pro" || m["theme"] != "dark" {
		t.Fatalf("settings: %v", m)
	}
	provs := m["providers"].(map[string]any)
	if _, ok := provs["openai"]; !ok {
		t.Fatalf("the user's provider is gone: %v", provs)
	}
	mp := provs["magpie"].(map[string]any)
	if mp["baseUrl"] != gatewayV1() || mp["api"] != "openai-chat" || mp["apiKey"] != "magpie-mothx" {
		t.Fatalf("magpie provider: %v", mp)
	}
	ms := mp["models"].([]any)
	if len(ms) != 1 || ms[0].(map[string]any)["id"] != "deepseek/pro" {
		t.Fatalf("models: %v", ms)
	}
	if got := f.Get(); got != "magpie/deepseek/pro" {
		t.Fatalf("get: %q", got)
	}
	if msg := a.Check(); msg != "" {
		t.Fatalf("check: %s", msg)
	}
	// the thinking level is MothX's defaultThinkingLevel
	if err := a.Field("effort").Set("high"); err != nil {
		t.Fatal(err)
	}
	if v, _ := edit.GetJSON(settings, "defaultThinkingLevel"); v != "high" {
		t.Fatalf("thinking: %q", v)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if ms, _ = readJSON(t, settings)["providers"].(map[string]any)["magpie"].(map[string]any)["models"].([]any); len(ms) != 1 {
		t.Fatalf("after sync: %v", ms)
	}

	// back to one of MothX's own: magpie is no longer the model, and the user
	// keeps their own provider
	if err := f.Set("openai/gpt-5"); err != nil {
		t.Fatal(err)
	}
	m = readJSON(t, settings)
	if m["defaultProvider"] != "openai" || m["defaultModel"] != "gpt-5" {
		t.Fatalf("after own: %v", m)
	}
	if got := f.Get(); got != "openai/gpt-5" {
		t.Fatalf("get: %q", got)
	}

	// MothX's default: magpie's provider and model come out, the user's stays
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	m = readJSON(t, settings)
	if m["defaultProvider"] != nil || m["defaultModel"] != nil {
		t.Fatalf("after default: %v", m)
	}
	if _, ok := m["providers"].(map[string]any)["magpie"]; ok {
		t.Fatalf("magpie's provider is still there: %v", m)
	}
	if _, ok := m["providers"].(map[string]any)["openai"]; !ok {
		t.Fatalf("the user's provider is gone: %v", m)
	}
}

// $MOTHX_DIR moves settings.json, and one magpie makes is
// {"providers":{"magpie":…}} with the model it picked.
func TestMothXNewFile(t *testing.T) {
	mothxHome(t)
	dir := filepath.Join(t.TempDir(), "m")
	t.Setenv("MOTHX_DIR", dir)
	a := mothx(t.TempDir())
	if a.Path != filepath.Join(dir, "settings.json") {
		t.Fatalf("path: %s", a.Path)
	}
	if err := a.Field("model").Set("magpie/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, a.Path)
	if m["defaultProvider"] != "magpie" || m["defaultModel"] != "deepseek/pro" {
		t.Fatalf("settings: %v", m)
	}
	mp := m["providers"].(map[string]any)["magpie"].(map[string]any)
	if mp["baseUrl"] != gatewayV1() || mp["apiKey"] != "magpie-mothx" {
		t.Fatalf("magpie provider: %v", mp)
	}
	if ms := mp["models"].([]any); len(ms) != 1 || ms[0].(map[string]any)["id"] != "deepseek/pro" {
		t.Fatalf("models: %v", mp["models"])
	}
}

// magpie's provider block keeps a key another tool added to it (Pi's
// pi-cache-optimizer writes one to Pi's; MothX's block is its own), while
// the model list is magpie's alone.
func TestMothXKeepsTheUsersProviderKeys(t *testing.T) {
	home := mothxHome(t)
	dir := filepath.Join(home, ".mothx")
	os.MkdirAll(dir, 0o755)
	settings := filepath.Join(dir, "settings.json")
	j := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	os.WriteFile(settings, []byte(`{"providers":{"magpie":{"baseUrl":"http://127.0.0.1:3425/v1","api":"openai-chat","apiKey":"magpie-mothx","httpProxy":"http://127.0.0.1:7890","models":[{"id":"gone/model"}]}}}`), 0o600)
	a := mothx(home)
	if err := a.Field("model").Set("magpie/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	mp := readJSON(t, settings)["providers"].(map[string]any)["magpie"].(map[string]any)
	if mp["httpProxy"] != "http://127.0.0.1:7890" {
		t.Fatalf("the user's key is gone: %v", mp)
	}
	if ms := mp["models"].([]any); len(ms) != 1 || ms[0].(map[string]any)["id"] != "deepseek/pro" {
		t.Fatalf("stale model kept: %s", j(mp["models"]))
	}
}
