package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/modelcatalog"
	"infinite-canvas/backend/internal/workspace"
)

// PlatformModelConfigStore is the canonical on-disk platform channel snapshot.
// It is deliberately narrow so the projection stays independent of the local
// desktop runtime that owns the same file for single-user installs.
type PlatformModelConfigStore interface {
	LoadEffectiveModelConfig() (workspace.EffectiveModelConfig, workspace.ConfigHealth, error)
}

// SyncPlatformModelConfig projects the canonical platform model configuration
// into system-scoped database channels. OrcaTV keeps a single administrator
// maintained configuration file, while every runtime query (routing, catalogs,
// generation) reads the encrypted system channel tables. Local desktop mode
// keeps its own on-disk-only semantics and is left untouched.
func (s *Service) SyncPlatformModelConfig(store PlatformModelConfigStore) error {
	if s == nil || s.repo == nil || store == nil {
		return nil
	}
	if s.IsLocalMode() {
		return nil
	}
	effective, _, err := store.LoadEffectiveModelConfig()
	if err != nil {
		return fmt.Errorf("读取平台模型配置失败: %w", err)
	}
	desired, err := s.projectPlatformChannels(effective.Config)
	if err != nil {
		return err
	}
	if err := s.replaceSystemPlatformChannels(desired); err != nil {
		return err
	}
	s.invalidateRouteCatalog()
	return nil
}

type projectedModelConfig struct {
	channel model.ModelChannel
	apiKey  string
	secret  string
	models  []model.ChannelModel
}

func (s *Service) projectPlatformChannels(config map[string]any) ([]projectedModelConfig, error) {
	rawChannels, _ := config["channels"].([]any)
	result := make([]projectedModelConfig, 0, len(rawChannels))
	for index, raw := range rawChannels {
		channelMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		channelID := strings.TrimSpace(stringValue(channelMap["id"]))
		if channelID == "" {
			return nil, fmt.Errorf("平台模型配置第 %d 个渠道缺少 ID", index+1)
		}
		baseURL := strings.TrimSpace(stringValue(channelMap["baseUrl"]))
		if baseURL == "" {
			return nil, fmt.Errorf("平台渠道 %s 缺少 Base URL", channelID)
		}
		name := strings.TrimSpace(stringValue(channelMap["name"]))
		if name == "" {
			name = channelID
		}
		enabled := true
		if value, ok := channelMap["enabled"].(bool); ok {
			enabled = value
		}
		headersJSON, err := modelcatalog.EncodeChannelHeadersJSON(nil)
		if err != nil {
			return nil, err
		}
		projected := projectedModelConfig{
			channel: model.ModelChannel{
				ID:          channelID,
				Scope:       model.ChannelScopeSystem,
				Enabled:     enabled,
				Name:        name,
				BaseURL:     strings.TrimRight(baseURL, "/"),
				APIFormat:   firstNonEmptyString(stringValue(channelMap["apiFormat"]), "openai"),
				SortOrder:   index,
				HeadersJSON: headersJSON,
			},
			apiKey: strings.TrimSpace(stringValue(channelMap["apiKey"])),
			secret: strings.TrimSpace(stringValue(channelMap["secretKey"])),
		}
		projected.channel.ModelsJSON = marshalModelKeys(channelMap["models"])
		projected.models, err = projectPlatformChannelModels(channelID, channelMap["modelProfiles"])
		if err != nil {
			return nil, err
		}
		result = append(result, projected)
	}
	return result, nil
}

func projectPlatformChannelModels(channelID string, rawProfiles any) ([]model.ChannelModel, error) {
	profiles, _ := rawProfiles.([]any)
	result := make([]model.ChannelModel, 0, len(profiles))
	seen := make(map[string]bool, len(profiles))
	for index, raw := range profiles {
		profile, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		modelKey := strings.TrimPrefix(strings.TrimSpace(stringValue(profile["model"])), "models/")
		if modelKey == "" || seen[modelKey] {
			continue
		}
		seen[modelKey] = true
		capability := strings.TrimSpace(stringValue(profile["capability"]))
		protocol := strings.TrimSpace(stringValue(profile["protocol"]))
		providerModelKey := strings.TrimPrefix(strings.TrimSpace(stringValue(profile["providerModelKey"])), "models/")
		if providerModelKey == "" {
			providerModelKey = modelKey
		}
		enabled := true
		if value, ok := profile["enabled"].(bool); ok {
			enabled = value
		}
		capabilityJSON := "{}"
		if value, ok := profile["capabilityConfig"]; ok && value != nil {
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("平台渠道 %s 的模型 %s 能力配置无效: %w", channelID, modelKey, err)
			}
			capabilityJSON = string(encoded)
		}
		result = append(result, model.ChannelModel{
			ChannelID:            channelID,
			ModelKey:             modelKey,
			ProviderModelKey:     providerModelKey,
			DisplayName:          firstNonEmptyString(stringValue(profile["displayName"]), modelKey),
			SortOrder:            index,
			Capability:           capability,
			Protocol:             model.ChannelInterfaceType(protocol),
			Enabled:              enabled,
			CapabilityConfigJSON: capabilityJSON,
		})
	}
	return result, nil
}

func (s *Service) replaceSystemPlatformChannels(desired []projectedModelConfig) error {
	existing, err := s.repo.SystemChannels(true)
	if err != nil {
		return err
	}
	existingByID := make(map[string]model.ModelChannel, len(existing))
	for _, channel := range existing {
		existingByID[channel.ID] = channel
	}
	desiredByID := make(map[string]bool, len(desired))
	for index := range desired {
		desiredByID[desired[index].channel.ID] = true
	}
	for index := range desired {
		entry := &desired[index]
		if current, ok := existingByID[entry.channel.ID]; ok {
			entry.channel.CreatedAt = current.CreatedAt
			entry.channel.UserID = current.UserID
			entry.channel.PublicAlias = current.PublicAlias
			if entry.apiKey == "" {
				apiKey, err := s.decryptSettingSecret(current.APIKey)
				if err != nil {
					return err
				}
				entry.apiKey = apiKey
			}
			if entry.secret == "" {
				secretKey, err := s.decryptSettingSecret(current.SecretKey)
				if err != nil {
					return err
				}
				entry.secret = secretKey
			}
		}
	}
	for index := range desired {
		entry := &desired[index]
		if err := s.encryptChannelSecretsForWrite(entry); err != nil {
			return err
		}
		if err := s.repo.Save(&entry.channel); err != nil {
			return err
		}
		items, err := s.repo.ChannelModels(entry.channel.ID, true)
		if err != nil {
			return err
		}
		if err := s.applyProjectedModels(entry.channel.ID, entry.models, items); err != nil {
			return err
		}
	}
	for id := range existingByID {
		if desiredByID[id] {
			continue
		}
		if err := s.repo.DeleteSystemChannel(id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) encryptChannelSecretsForWrite(entry *projectedModelConfig) error {
	apiKey, err := s.encryptSettingSecret(entry.apiKey)
	if err != nil {
		return err
	}
	secretKey, err := s.encryptSettingSecret(entry.secret)
	if err != nil {
		return err
	}
	entry.channel.APIKey = apiKey
	entry.channel.SecretKey = secretKey
	return nil
}

func (s *Service) applyProjectedModels(channelID string, desired []model.ChannelModel, existing []model.ChannelModel) error {
	byKey := make(map[string]model.ChannelModel, len(existing))
	for _, item := range existing {
		byKey[item.ModelKey] = item
	}
	desiredKeys := make(map[string]bool, len(desired))
	for index := range desired {
		item := &desired[index]
		desiredKeys[item.ModelKey] = true
		if current, ok := byKey[item.ModelKey]; ok {
			item.ID = current.ID
			item.CreatedAt = current.CreatedAt
		} else {
			id, err := s.repo.NextPrefixedID("MODEL")
			if err != nil {
				return err
			}
			item.ID = id
		}
		item.ChannelID = channelID
		if err := s.repo.SaveChannelModel(item); err != nil {
			return err
		}
	}
	for _, item := range existing {
		if desiredKeys[item.ModelKey] || !item.Enabled {
			continue
		}
		item.Enabled = false
		if err := s.repo.SaveChannelModel(&item); err != nil {
			return err
		}
	}
	return nil
}

func marshalModelKeys(raw any) string {
	items, _ := raw.([]any)
	keys := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		key := strings.TrimPrefix(strings.TrimSpace(stringValue(item)), "models/")
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	encoded, err := json.Marshal(keys)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}
