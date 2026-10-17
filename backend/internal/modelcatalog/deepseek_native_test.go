package modelcatalog

import (
	"context"
	"testing"

	"infinite-canvas/backend/internal/beefapi"
	"infinite-canvas/backend/internal/model"
)

func TestNativeDeepSeekUpstreamModelMapsFlashAliasesOnly(t *testing.T) {
	for _, alias := range []string{"deepseek-v4.1-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp", " models/deepseek-flash "} {
		if got := NativeDeepSeekUpstreamModel(alias); got != NativeDeepSeekFlashModel {
			t.Fatalf("NativeDeepSeekUpstreamModel(%q) = %q", alias, got)
		}
	}
	if got := NativeDeepSeekUpstreamModel("deepseek-v4-pro"); got != "deepseek-v4-pro" {
		t.Fatalf("pro id changed: %q", got)
	}
	if got := ApplyNativeDeepSeekUpstreamModel("", "", "https://example.com/v1", "deepseek-v4.1-flash"); got != "deepseek-v4.1-flash" {
		t.Fatalf("non-native channel rewritten: %q", got)
	}
	if got := ApplyNativeDeepSeekUpstreamModel(beefapi.ChannelID, "", "https://api.deepseek.com", "deepseek-v4.1-flash"); got != "deepseek-v4.1-flash" {
		t.Fatalf("beefapi sku rewritten: %q", got)
	}
	if got := ApplyNativeDeepSeekUpstreamModel("custom", beefapi.CredentialRef, "https://api.deepseek.com/v1", "deepseek-v4.1-flash"); got != "deepseek-v4.1-flash" {
		t.Fatalf("beefapi credential rewritten: %q", got)
	}
	if got := ApplyNativeDeepSeekUpstreamModel("custom", "", "https://api.deepseek.com/anthropic", "deepseek-v4.1-flash"); got != NativeDeepSeekFlashModel {
		t.Fatalf("official host = %q", got)
	}
	if IsNativeDeepSeekBaseURL("https://api.deepseek.com.evil.example/v1") {
		t.Fatal("lookalike host treated as official")
	}
}

func TestAnnotateNativeDeepSeekCatalogDoesNotInventFlash(t *testing.T) {
	items, err := LoadChannelModelCatalog(context.Background(), func(context.Context, string, string, string, []ChannelHeader) ([]byte, error) {
		return []byte(`{"data":[{"id":"deepseek-v4-pro"},{"id":"deepseek-flash"}]}`), nil
	}, "https://api.deepseek.com", "openai", "synthetic", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "deepseek-flash" || items[1].ID != "deepseek-v4-pro" {
		t.Fatalf("catalog ids = %#v", items)
	}
	if items[0].DisplayName != NativeDeepSeekFlashDisplayName || items[0].ModelType != "text" {
		t.Fatalf("flash annotation = %#v", items[0])
	}
	for _, item := range items {
		if item.ID == "deepseek-v4.1-flash" {
			t.Fatal("invented beefapi sku")
		}
	}

	missing, err := LoadChannelModelCatalog(context.Background(), func(context.Context, string, string, string, []ChannelHeader) ([]byte, error) {
		return []byte(`{"data":[{"id":"deepseek-v4-pro"}]}`), nil
	}, "https://api.deepseek.com/v1", "openai", "synthetic", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0].ID != "deepseek-v4-pro" || missing[0].DisplayName != "" {
		t.Fatalf("missing flash catalog = %#v", missing)
	}
}

func TestSelectTaskModelRewritesNativeDeepSeekFlashAndKeepsOtherPools(t *testing.T) {
	native, err := SelectTaskModel(TaskSelectRequest{
		Input: map[string]any{"config": map[string]any{
			"baseUrl": "https://api.deepseek.com/v1", "apiKey": "synthetic", "interfaceType": "deepseek-chat",
			"model": "deepseek-v4.1-flash", "providerModelKey": "deepseek-v4-flash",
		}},
		Type: "canvas_text", FrontendEnabled: true,
	}, TaskSelectLookup{})
	if err != nil {
		t.Fatal(err)
	}
	config := native.Input["config"].(map[string]any)
	if config["model"] != NativeDeepSeekFlashModel || config["providerModelKey"] != NativeDeepSeekFlashModel {
		t.Fatalf("native request = %#v", config)
	}

	pro, err := SelectTaskModel(TaskSelectRequest{
		Input: map[string]any{"config": map[string]any{
			"baseUrl": "https://api.deepseek.com", "apiKey": "synthetic", "model": "deepseek-v4-pro",
		}},
		Type: "canvas_text", FrontendEnabled: true,
	}, TaskSelectLookup{})
	if err != nil {
		t.Fatal(err)
	}
	if pro.Input["config"].(map[string]any)["model"] != "deepseek-v4-pro" {
		t.Fatalf("pro rewritten: %#v", pro.Input["config"])
	}

	pool, err := SelectTaskModel(TaskSelectRequest{
		Input: map[string]any{"config": map[string]any{
			"channelId": beefapi.ChannelID, "credentialRef": beefapi.CredentialRef,
			"baseUrl": "https://api.deepseek.com", "apiKey": "synthetic", "model": "deepseek-v4.1-flash",
		}},
		Type: "canvas_text", FrontendEnabled: true,
	}, TaskSelectLookup{})
	if err != nil {
		t.Fatal(err)
	}
	if pool.Input["config"].(map[string]any)["model"] != "deepseek-v4.1-flash" {
		t.Fatalf("beefapi sku rewritten: %#v", pool.Input["config"])
	}
}

func TestSystemChannelNativeDeepSeekUsesOfficialFlashKey(t *testing.T) {
	profile := DefaultModelCapabilityConfigForModel(string(model.ChannelInterfaceChatCompletion), "deepseek-v4.1-flash")
	channel, channelModel, lookup := selectionLookup(t, "text", model.ChannelInterfaceChatCompletion, profile, []model.ChannelModelVariant{
		{ID: "tier-default", SelectorKey: `{}`, SelectorJSON: `{}`, ProviderModelKey: "deepseek-v4.1-flash", Enabled: true},
	})
	channel.BaseURL = "https://api.deepseek.com"
	channelModel.ModelKey = "deepseek-v4.1-flash"
	channelModel.ProviderModelKey = "deepseek-v4.1-flash"
	result, err := SelectTaskModel(TaskSelectRequest{
		Input: map[string]any{"config": map[string]any{"channelId": channel.ID, "model": "deepseek-v4.1-flash", "channelModelKey": "deepseek-v4.1-flash"}},
		Type:  "canvas_text",
	}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	config := result.Input["config"].(map[string]any)
	if config["model"] != "deepseek-v4.1-flash" || config["providerModelKey"] != NativeDeepSeekFlashModel {
		t.Fatalf("system native key = %#v", config)
	}

	channel.ID = beefapi.ChannelID
	kept, err := SelectTaskModel(TaskSelectRequest{
		Input: map[string]any{"config": map[string]any{"channelId": channel.ID, "model": "deepseek-v4.1-flash"}},
		Type:  "canvas_text",
	}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Input["config"].(map[string]any)["providerModelKey"] != "deepseek-v4.1-flash" {
		t.Fatalf("beefapi system sku = %#v", kept.Input["config"])
	}
}

func TestResolveAssistantUsesOfficialFlashOnNativeDeepSeek(t *testing.T) {
	native := AssistantConfigSnapshot{Channels: []AssistantChannel{{
		ID: "deepseek", Enabled: true, APIKey: "synthetic", BaseURL: "https://api.deepseek.com",
		Models: []string{"deepseek-flash", "deepseek-v4-pro"},
		ModelProfiles: []AssistantModelProfile{
			{Model: "deepseek-flash", Capability: "text", Protocol: "deepseek-chat"},
			{Model: "deepseek-v4-pro", Capability: "text", Protocol: "deepseek-chat"},
		},
	}}}
	got, reason := ResolveAssistantChannelModel(native, "deepseek::deepseek-v4.1-flash", nil)
	if reason != "" || got.Model != NativeDeepSeekFlashModel || got.Protocol != "chat-completion" {
		t.Fatalf("alias request = %#v reason=%s", got, reason)
	}
	pro, reason := ResolveAssistantChannelModel(native, "deepseek::deepseek-v4-pro", nil)
	if reason != "" || pro.Model != "deepseek-v4-pro" {
		t.Fatalf("pro request = %#v reason=%s", pro, reason)
	}

	beef := AssistantConfigSnapshot{Channels: []AssistantChannel{{
		ID: beefapi.ChannelID, Pinned: true, Enabled: true, CredentialRef: beefapi.CredentialRef,
		APIKey: "synthetic", BaseURL: "https://beeftv.app",
		Models:        []string{"deepseek-v4.1-flash"},
		ModelProfiles: []AssistantModelProfile{{Model: "deepseek-v4.1-flash", Capability: "text", Protocol: "chat-completion"}},
	}}}
	pool, reason := ResolveAssistantChannelModel(beef, "beefapi::deepseek-v4.1-flash", nil)
	if reason != "" || pool.Model != "deepseek-v4.1-flash" {
		t.Fatalf("beefapi assistant = %#v reason=%s", pool, reason)
	}
}
