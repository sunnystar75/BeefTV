package modelcatalog

import (
	"net/url"
	"strings"

	"infinite-canvas/backend/internal/beefapi"
)

const (
	// NativeDeepSeekFlashModel is the official api.deepseek.com id for
	// DeepSeek-V4.1-Flash. BeefAPI's enterprise SKU stays deepseek-v4.1-flash.
	NativeDeepSeekFlashModel       = "deepseek-flash"
	NativeDeepSeekFlashDisplayName = "DeepSeek V4.1 Flash"
	nativeDeepSeekHost             = "api.deepseek.com"
)

// IsNativeDeepSeekBaseURL reports the official DeepSeek API host. Lookalike
// hosts and other providers are left unchanged.
func IsNativeDeepSeekBaseURL(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSuffix(parsed.Hostname(), "."), nativeDeepSeekHost)
}

func nativeDeepSeekMappingAllowed(channelID, credentialRef, baseURL string) bool {
	if strings.TrimSpace(channelID) == beefapi.ChannelID {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(credentialRef), beefapi.CredentialRef) {
		return false
	}
	return IsNativeDeepSeekBaseURL(baseURL)
}

// NativeDeepSeekUpstreamModel maps BeefTV and retired Flash aliases to the
// official id. deepseek-v4-pro and every other id are returned unchanged.
func NativeDeepSeekUpstreamModel(model string) string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(model), "models/")
	switch strings.ToLower(trimmed) {
	case "deepseek-v4.1-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp", NativeDeepSeekFlashModel:
		return NativeDeepSeekFlashModel
	default:
		return trimmed
	}
}

// ApplyNativeDeepSeekUpstreamModel rewrites a model id only for the official
// DeepSeek host. BeefAPI and every other channel keep the caller's id.
func ApplyNativeDeepSeekUpstreamModel(channelID, credentialRef, baseURL, model string) string {
	if !nativeDeepSeekMappingAllowed(channelID, credentialRef, baseURL) {
		return model
	}
	mapped := NativeDeepSeekUpstreamModel(model)
	if mapped == "" {
		return model
	}
	return mapped
}

// NativeDeepSeekModelKeyTrusted accepts the official Flash id when the stored
// system-channel SKU is one of its aliases. Other models stay on the normal
// identity check.
func NativeDeepSeekModelKeyTrusted(channelID, baseURL, sku, authorized, requestedModel, requestedProviderKey string) bool {
	if !nativeDeepSeekMappingAllowed(channelID, "", baseURL) {
		return false
	}
	if NativeDeepSeekUpstreamModel(sku) != NativeDeepSeekFlashModel && NativeDeepSeekUpstreamModel(authorized) != NativeDeepSeekFlashModel {
		return false
	}
	return nativeFlashKeyAccepted(requestedModel, sku, authorized) && nativeFlashKeyAccepted(requestedProviderKey, sku, authorized)
}

func nativeFlashKeyAccepted(value, sku, authorized string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == strings.TrimSpace(sku) || value == strings.TrimSpace(authorized) {
		return true
	}
	return NativeDeepSeekUpstreamModel(value) == NativeDeepSeekFlashModel
}

// AnnotateNativeDeepSeekCatalog labels deepseek-flash when the official
// /models list returned it. It never inserts a model the payload omitted.
func AnnotateNativeDeepSeekCatalog(baseURL string, items []ChannelModelCatalogItem) []ChannelModelCatalogItem {
	if !IsNativeDeepSeekBaseURL(baseURL) {
		return items
	}
	for index := range items {
		if strings.TrimSpace(items[index].ID) != NativeDeepSeekFlashModel {
			continue
		}
		if strings.TrimSpace(items[index].DisplayName) == "" {
			items[index].DisplayName = NativeDeepSeekFlashDisplayName
		}
		if strings.TrimSpace(items[index].ModelType) == "" {
			items[index].ModelType = "text"
		}
	}
	return items
}
