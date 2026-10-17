import { expect, test } from "bun:test";

import { mergeFetchedChannelModelProfiles } from "../src/lib/channel-model-catalog";
import { applyFetchedChannelModelCatalog } from "../src/pages/settings/channel-settings-pane";
import { assistantModelOptions } from "../src/lib/assistant-model";
import { createModelChannel, defaultConfig, resolveModelRequestConfig } from "../src/stores/use-config-store";

test("native DeepSeek refresh keeps deepseek-flash and does not invent the BeefAPI sku", () => {
    const channel = createModelChannel({
        id: "deepseek",
        baseUrl: "https://api.deepseek.com/v1",
        interfaceType: "deepseek-chat",
        models: ["deepseek-v4-pro"],
        modelProfiles: [{ model: "deepseek-v4-pro", capability: "text", protocol: "deepseek-chat" }],
    });
    const pulled = applyFetchedChannelModelCatalog(channel, {
        models: ["deepseek-flash", "deepseek-v4-pro"],
        catalog: [{ id: "deepseek-flash" }, { id: "deepseek-v4-pro" }],
    });
    expect(pulled.models).toEqual(["deepseek-v4-pro", "deepseek-flash"]);
    expect(pulled.models).not.toContain("deepseek-v4.1-flash");
    expect(pulled.modelProfiles?.find((profile) => profile.model === "deepseek-flash")).toMatchObject({
        capability: "text",
        protocol: "deepseek-chat",
        displayName: "DeepSeek V4.1 Flash",
    });

    const absent = applyFetchedChannelModelCatalog(channel, {
        models: ["deepseek-v4-pro"],
        catalog: [{ id: "deepseek-v4-pro" }],
    });
    expect(absent.models).toEqual(["deepseek-v4-pro"]);
    expect(mergeFetchedChannelModelProfiles(channel, [{ id: "deepseek-v4-pro" }]).some((profile) => profile.model === "deepseek-flash")).toBe(false);
});

test("native DeepSeek chat sends deepseek-flash while BeefAPI keeps its sku", () => {
    const native = createModelChannel({
        id: "deepseek",
        baseUrl: "https://api.deepseek.com",
        interfaceType: "deepseek-chat",
        models: ["deepseek-v4.1-flash", "deepseek-v4-pro"],
        modelProfiles: [
            { model: "deepseek-v4.1-flash", capability: "text", protocol: "deepseek-chat" },
            { model: "deepseek-v4-pro", capability: "text", protocol: "deepseek-chat" },
        ],
    });
    const config = { ...defaultConfig, channels: [native], model: "deepseek::deepseek-v4.1-flash" };
    expect(resolveModelRequestConfig(config, config.model).model).toBe("deepseek-flash");
    expect(resolveModelRequestConfig(config, "deepseek::deepseek-v4-pro").model).toBe("deepseek-v4-pro");
    expect(resolveModelRequestConfig({ ...config, model: "deepseek::deepseek-v4-flash" }, "deepseek::deepseek-v4-flash").model).toBe("deepseek-flash");

    const beef = createModelChannel({
        id: "beefapi",
        pinned: true,
        credentialRef: "beefapi-enterprise",
        baseUrl: "https://api.deepseek.com",
        models: ["deepseek-v4.1-flash"],
        modelProfiles: [{ model: "deepseek-v4.1-flash", capability: "text", protocol: "chat-completion" }],
    });
    const pool = { ...defaultConfig, channels: [beef], model: "beefapi::deepseek-v4.1-flash" };
    expect(resolveModelRequestConfig(pool, pool.model).model).toBe("deepseek-v4.1-flash");
    expect(assistantModelOptions({ ...config, assistantModel: "", textModel: "deepseek::deepseek-flash", channels: [{ ...native, models: ["deepseek-flash", "deepseek-v4-pro"], modelProfiles: [{ model: "deepseek-flash", capability: "text", protocol: "deepseek-chat" }, { model: "deepseek-v4-pro", capability: "text", protocol: "deepseek-chat" }] }] })).toContain("deepseek::deepseek-flash");
});
