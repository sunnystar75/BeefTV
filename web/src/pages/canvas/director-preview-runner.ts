export const DIRECTOR_PREVIEW_KIND = "director_preview";
export const DIRECTOR_CLAY_MODEL_KEY = "local::director-clay-preview";

export type DirectorPreviewDispatchItem = {
    operationId: string;
    proposalId: string;
    canvasId: string;
    nodeIds: string[];
    kind: string;
    modelKey: string;
    charged?: boolean;
};

type RunDirectorPreviewOptions = {
    nodeIds: string[];
    openDirector: (nodeId: string) => boolean;
    exportFor: (nodeId: string) => (() => Promise<void>) | null;
    wait?: (ms: number) => Promise<void>;
    timeoutMs?: number;
    now?: () => number;
};

const defaultWait = (ms: number) => new Promise<void>((resolve) => {
    setTimeout(resolve, ms);
});

/** 打开导演台并调用当前白膜导出。桌面确认和面板上的「导出白膜」共用这一条路径。 */
export async function runDirectorPreviewNodes({
    nodeIds,
    openDirector,
    exportFor,
    wait = defaultWait,
    timeoutMs = 15_000,
    now = () => Date.now(),
}: RunDirectorPreviewOptions) {
    const targets = [...new Set(nodeIds.map((id) => id.trim()).filter(Boolean))];
    if (!targets.length) throw new Error("白膜确认没有导演节点");
    for (const nodeId of targets) {
        if (!openDirector(nodeId)) throw new Error("这个节点不是导演台，白膜导出没有开始");
        const deadline = now() + timeoutMs;
        for (;;) {
            const run = exportFor(nodeId);
            if (run) {
                await run();
                break;
            }
            if (now() >= deadline) throw new Error("导演台还没准备好，白膜导出没有开始");
            await wait(50);
        }
    }
}

export function isDirectorPreviewDispatch(item: DirectorPreviewDispatchItem) {
    return item.kind === DIRECTOR_PREVIEW_KIND && item.charged !== true && item.modelKey === DIRECTOR_CLAY_MODEL_KEY && item.operationId.trim() !== "" && item.nodeIds.some((id) => id.trim());
}

/**
 * 已经开始录制的确认只补 ack，避免同一次 CLI 确认录两段。
 * 录制本身失败时放开认领，下一次轮询可以再试。
 */
export async function consumeDirectorPreviewDispatches(items: DirectorPreviewDispatchItem[], deps: {
    started: Set<string>;
    run: (nodeIds: string[]) => Promise<void>;
    ack: (item: DirectorPreviewDispatchItem) => Promise<void>;
}) {
    for (const item of items) {
        if (!isDirectorPreviewDispatch(item)) continue;
        if (deps.started.has(item.operationId)) {
            await deps.ack(item);
            continue;
        }
        deps.started.add(item.operationId);
        try {
            await deps.run(item.nodeIds);
        } catch (error) {
            deps.started.delete(item.operationId);
            throw error;
        }
        await deps.ack(item);
    }
}
