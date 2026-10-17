export const NATIVE_DEEPSEEK_FLASH_MODEL = "deepseek-flash";
export const NATIVE_DEEPSEEK_FLASH_LABEL = "DeepSeek V4.1 Flash";

const NATIVE_DEEPSEEK_FLASH_ALIASES = new Set(["deepseek-v4.1-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp", NATIVE_DEEPSEEK_FLASH_MODEL]);

type NativeDeepSeekChannel = { id?: string; baseUrl?: string; credentialRef?: string; interfaceType?: string };

export function isNativeDeepSeekBaseURL(baseURL: string) {
    try {
        return new URL(baseURL.trim()).hostname.replace(/\.$/, "").toLowerCase() === "api.deepseek.com";
    } catch {
        return false;
    }
}

export function isNativeDeepSeekChannel(channel: NativeDeepSeekChannel) {
    if (channel.id === "beefapi" || channel.credentialRef === "beefapi-enterprise") return false;
    return isNativeDeepSeekBaseURL(channel.baseUrl || "");
}

export function nativeDeepSeekUpstreamModel(model: string) {
    const trimmed = model.trim().replace(/^models\//, "");
    return NATIVE_DEEPSEEK_FLASH_ALIASES.has(trimmed.toLowerCase()) ? NATIVE_DEEPSEEK_FLASH_MODEL : trimmed;
}

export function nativeDeepSeekRequestModel(channel: NativeDeepSeekChannel, model: string) {
    if (!isNativeDeepSeekChannel(channel)) return model;
    const mapped = nativeDeepSeekUpstreamModel(model);
    return mapped || model;
}

export function annotateNativeDeepSeekCatalogItem<T extends { id: string; displayName?: string; modelType?: "text" | "image" | "video" | "audio" }>(channel: NativeDeepSeekChannel, item: T): T {
    if (!isNativeDeepSeekChannel(channel) || item.id.trim() !== NATIVE_DEEPSEEK_FLASH_MODEL) return item;
    return {
        ...item,
        ...(item.displayName?.trim() ? {} : { displayName: NATIVE_DEEPSEEK_FLASH_LABEL }),
        ...(item.modelType ? {} : { modelType: "text" as const }),
    };
}
