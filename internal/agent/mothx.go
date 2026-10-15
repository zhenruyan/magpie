package agent

// MothX (mothx, formerly VibeCoding's vibecoding CLI) is a terminal coding
// agent that reads its providers and its startup model from one settings.json
// ($MOTHX_DIR if set, else ~/.mothx/settings.json):
//
//	{"defaultProvider":"magpie","defaultModel":"deepseek/v4-pro",
//	 "defaultThinkingLevel":"high",
//	 "providers":{"magpie":{"baseUrl":"http://127.0.0.1:3425/v1",
//	   "api":"openai-chat","apiKey":"magpie-mothx",
//	   "models":[{"id":"deepseek/v4-pro","name":…,"reasoning":true,
//	     "contextWindow":…,"maxTokens":…,"cost":{…},"input":["text"]}]}}}
//
// A session asks provider <defaultProvider> for model <defaultModel> on the
// provider's own api (openai-chat here, so the gateway's
// /v1/chat/completions), under Bearer <apiKey>. Its requests carry the
// generic "Vibecoding Client/<ver>" User-Agent, so they are told apart by
// the key magpie gives it (magpie-mothx, gateway.TokenFor). It reads
// settings.json at start-up.

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/edit"
)

// mothxLevels are the thinking levels MothX offers (its provider.ThinkingLevel),
// the same set Pi has.
var mothxLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

func mothx(home string) *Agent { return mothxIn(here(home)) }

// mothxIn is MothX at a place: this machine's home, or a WSL distro's (see
// wsl.go), its settings.json naming the gateway as it reaches it from there.
func mothxIn(at place) *Agent {
	dir := mothxDir(at)
	path := filepath.Join(dir, "settings.json")
	get := func(k string) (string, bool) { return edit.GetJSON(path, k) }
	set := func(kvs ...edit.KV) error { return edit.SetJSON(path, kvs...) }
	pair := pairSet(set, "defaultProvider", "defaultModel")
	block := theirsKept(path, "providers."+magpieID, func() any { return mothxProvider(at) }, "models")
	writeMagpie := func() error {
		return edit.SetJSON(path, edit.KV{Path: "providers." + magpieID, Value: block()})
	}
	return atomic(&Agent{
		ID: "mothx", Name: "MothX", Icon: "mothx", Bin: "mothx", Dir: dir, Path: path, Spelled: prefixed,
		Aliases: []string{"vibecoding"},
		UA:      []string{"vibecoding", "mothx"},
		Check: func() string {
			if p, _ := get("defaultProvider"); p != magpieID {
				return ""
			}
			return wiringOff("MothX", path, func(k string) (string, bool) { return edit.GetJSON(path, "providers."+magpieID+"."+k) },
				"baseUrl", at.v1(), "apiKey", agentKeyAt("mothx", at.gw()))
		},
		Sync: func() error {
			return syncJSON(path, "providers."+magpieID, block)
		},
		// MothX reads settings.json at start-up
		Notice: func() string {
			if p, _ := get("defaultProvider"); p == magpieID && Running(`(^|/)mothx( |$)`, `(^|/)vibecoding( |$)`) {
				return "MothX reads its providers at start-up — restart open mothx sessions to use this."
			}
			return ""
		},
		Fields: []Field{
			{
				Key: "model", Label: "model",
				// defaultProvider/defaultModel, as one provider/model value
				Get: func() string { return pairGet(get, "defaultProvider", "defaultModel")() },
				Set: func(v string) error {
					if v == "" {
						if err := edit.DelJSON(path, "defaultProvider", "defaultModel"); err != nil {
							return err
						}
						return edit.DelJSON(path, "providers."+magpieID)
					}
					// a model of magpie's: its provider block goes in beside the pick
					if ref, ok := strings.CutPrefix(v, magpieID+"/"); ok && isMagpie(ref) {
						if err := writeMagpie(); err != nil {
							return err
						}
					}
					return pair(v)
				},
				Options: func(cur map[string]string) []Option {
					var out []Option
					if v := cur["model"]; v != "" && !usesMagpie(v) {
						out = append(out, Option{Value: v, Icon: modelIcon("", v)})
					}
					return append(out, viaMagpie("mothx", magpieID+"/")...)
				},
			},
			{
				// MothX's startup thinking level, its provider.ThinkingLevel
				// list; a model that doesn't reason asks for none of it
				Key: "effort", Label: "thinking",
				Get: func() string { v, _ := get("defaultThinkingLevel"); return v },
				Set: func(v string) error {
					if v == "" {
						return edit.DelJSON(path, "defaultThinkingLevel")
					}
					return set(edit.KV{Path: "defaultThinkingLevel", Value: v})
				},
				Options: func(cur map[string]string) []Option {
					if offered := mothxOffered("mothx", cur["model"]); offered != nil {
						return static(offered...)
					}
					return static(mothxLevels...)
				},
			},
		},
	}, path)
}

// mothxDir is MothX's config folder at a place: $MOTHX_DIR, else %APPDATA%\mothx
// on Windows (platform.ConfigDir) or ~/.mothx elsewhere; a distro's variables
// aren't read, so it is ~/.mothx there.
func mothxDir(at place) string {
	if d := at.getenv("MOTHX_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" && at.spell == nil {
		if app := appdir.Getenv("APPDATA"); app != "" {
			return filepath.Join(app, "mothx")
		}
	}
	return filepath.Join(at.home, ".mothx")
}

// mothxProvider is magpie as MothX's provider block: one model table per
// catalog model (openai-chat through the gateway's /v1), the key the
// gateway knows MothX by, and its name as a provider id ("magpie").
func mothxProvider(at place) any {
	var ms []map[string]any
	for _, m := range magpieModels("mothx") {
		ms = append(ms, mothxModelJSON(m))
	}
	if ms == nil {
		ms = []map[string]any{}
	}
	return map[string]any{"baseUrl": at.v1(), "api": "openai-chat",
		"apiKey": agentKeyAt("mothx", at.gw()), "models": ms}
}

// mothxModelJSON is one of magpie's models as a MothX provider's model
// table entry, as its own presets write one.
func mothxModelJSON(m catalog.Model) map[string]any {
	e := map[string]any{"id": m.ID, "name": m.Name, "reasoning": m.Reasoning || len(m.Efforts) > 0}
	// text alone, and images when magpie takes them: without it MothX reads
	// the model as supporting no input at all
	in := []string{"text"}
	if m.Images {
		in = append(in, "image")
	}
	e["input"] = in
	// without it MothX takes every model for its own default window and caps
	// a reply at its own default
	if m.Context > 0 {
		e["contextWindow"] = m.Context
	}
	if m.Output > 0 {
		e["maxTokens"] = maxTokens(m)
	}
	// MothX shows a call's cost by it
	if m.Price != nil {
		e["cost"] = map[string]any{"input": m.Price.Input, "output": m.Price.Output,
			"cacheRead": m.Price.CacheRead, "cacheWrite": m.Price.CacheWrite}
	}
	return e
}

// mothxOffered are the thinking levels MothX offers for model, when it is
// one of magpie's and magpie knows its levels; nil for any other model,
// whose levels are MothX's own business — and for one whose levels magpie
// doesn't know, where MothX's own list is offered.
func mothxOffered(agentID, model string) []string {
	ref, ok := strings.CutPrefix(model, magpieID+"/")
	if !ok {
		return nil
	}
	for _, m := range magpieModels(agentID) {
		if m.ID != ref || len(m.Efforts) == 0 {
			continue
		}
		var out []string
		for _, e := range m.Efforts {
			if e == "none" {
				e = "off"
			}
			if slices.Contains(mothxLevels, e) && !slices.Contains(out, e) {
				out = append(out, e)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}
