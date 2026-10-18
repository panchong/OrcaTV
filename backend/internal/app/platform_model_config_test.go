package app

import (
	"encoding/json"
	"testing"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/modelcatalog"
)

// 平台渠道配置里的文本模型往往只声明 capability/protocol，没有 capabilityConfig。
// 投影时必须回退到协议默认能力，否则渠道模型以空配置落库后所有生成都会被拒绝。
func TestProjectPlatformChannelModelsDefaultsTextCapability(t *testing.T) {
	profiles := []any{
		map[string]any{"model": "deepseek-v4-pro", "capability": "text", "protocol": "chat-completion"},
	}
	models, err := projectPlatformChannelModels("system-text", profiles)
	if err != nil {
		t.Fatalf("projectPlatformChannelModels() error = %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("projected models = %d, want 1", len(models))
	}
	item := models[0]
	if item.CapabilityConfigJSON == "" || item.CapabilityConfigJSON == "{}" {
		t.Fatalf("text model capability config was not defaulted: %q", item.CapabilityConfigJSON)
	}
	summary, err := modelcatalog.SanitizeChannelModel(&item)
	if err != nil {
		t.Fatalf("projected text model is not admissible: %v", err)
	}
	if summary.CapabilityConfig == nil {
		t.Fatalf("projected text model lost its capability config: %#v", summary)
	}
	intent := modelcatalog.ModelRequestIntent{Capability: "text", Options: map[string]any{}}
	matched, err := modelcatalog.ChannelModelMatchesIntent(&item, &intent)
	if err != nil {
		t.Fatalf("ChannelModelMatchesIntent() error = %v", err)
	}
	if !matched {
		t.Fatalf("projected text model does not match a text intent")
	}
}

func TestProjectPlatformChannelModelsKeepsExplicitCapabilityConfig(t *testing.T) {
	profiles := []any{
		map[string]any{
			"model": "gpt-image-2", "capability": "image", "protocol": "openai-image",
			"capabilityConfig": map[string]any{"image": map[string]any{"maxOutputs": 3}},
		},
	}
	models, err := projectPlatformChannelModels("system-image", profiles)
	if err != nil {
		t.Fatalf("projectPlatformChannelModels() error = %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("projected models = %d, want 1", len(models))
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(models[0].CapabilityConfigJSON), &decoded); err != nil {
		t.Fatalf("capability config is not valid JSON: %v", err)
	}
	image, _ := decoded["image"].(map[string]any)
	if image == nil || image["maxOutputs"] != float64(3) {
		t.Fatalf("explicit capability config was overwritten: %s", models[0].CapabilityConfigJSON)
	}
}

func TestProjectPlatformChannelModelsIgnoresClientModelKeyPrefix(t *testing.T) {
	profiles := []any{
		map[string]any{"model": "models/deepseek-v4-pro", "capability": "text", "protocol": string(model.ChannelInterfaceChatCompletion)},
	}
	models, err := projectPlatformChannelModels("system-text", profiles)
	if err != nil {
		t.Fatalf("projectPlatformChannelModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ModelKey != "deepseek-v4-pro" {
		t.Fatalf("unexpected projected models: %#v", models)
	}
}
