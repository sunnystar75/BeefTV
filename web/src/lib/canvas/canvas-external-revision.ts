import { mergeCanvasViewportPreference } from "@/lib/canvas/canvas-viewport-preference";
import type { CanvasProject } from "@/stores/canvas/use-canvas-store";

export type CanvasExternalRevisionDecision =
    | { kind: "apply"; project: CanvasProject; localRevision: number; remoteRevision: number }
    | { kind: "keep-local"; projectId: string; candidate: CanvasProject; localRevision: number; remoteRevision: number };

/**
 * 外部写入（内置助手回合、CLI/MCP 操作）到达时，决定是否把它投影到本地编辑器。
 *
 * 只有本地没有服务端尚未确认的编辑时才允许应用；有编辑时保留本地内容，并把
 * 外部内容作为候选原样留下（供用户显式选择），因为外部文档是整份覆盖写，
 * 直接套用会回滚用户正在编辑的内容。
 */
export function decideExternalCanvasRevision(input: {
    remote: CanvasProject;
    local: CanvasProject | undefined;
    hasLocalEdits: boolean;
}): CanvasExternalRevisionDecision {
    const { remote, local, hasLocalEdits } = input;
    const remoteRevision = remote.revision ?? 0;
    if (!local) return { kind: "apply", project: remote, localRevision: 0, remoteRevision };
    const localRevision = local.revision ?? 0;
    if (hasLocalEdits) return { kind: "keep-local", projectId: remote.id, candidate: remote, localRevision, remoteRevision };
    return { kind: "apply", project: mergeExternalCanvasRevision(remote, local), localRevision, remoteRevision };
}

/**
 * 外部文档是服务端权威内容。普通写入不改视口，套用时保留本机视角。
 * viewportSeq 只由视口操作递增；更高的序号表示一次显式对准或设视角。
 */
function mergeExternalCanvasRevision(remote: CanvasProject, local: CanvasProject): CanvasProject {
    const view = mergeCanvasViewportPreference(local, remote);
    return {
        ...remote,
        viewport: view.viewport,
        viewportSeq: view.viewportSeq,
        appearance: local.appearance || remote.appearance,
        backgroundMode: local.backgroundMode || remote.backgroundMode,
    };
}
