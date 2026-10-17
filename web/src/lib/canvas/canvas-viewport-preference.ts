import type { ViewportTransform } from "@/types/canvas";

type ViewportPreference = {
    viewport: ViewportTransform;
    viewportSeq?: number;
};

/** viewportSeq 只由视口操作递增。缺省、非整数和非正数都视为还没有显式视口写入。 */
export function canvasViewportSeq(project: { viewportSeq?: number } | null | undefined): number {
    const seq = project?.viewportSeq;
    return typeof seq === "number" && Number.isInteger(seq) && seq > 0 ? seq : 0;
}

/**
 * 序号更高的一侧是一次显式对准或设视角，采用它。
 * 序号没有前进时保留本地平移，普通文档刷新不会把相机拉回去。
 */
export function mergeCanvasViewportPreference<T extends ViewportPreference>(local: T, remote: T): ViewportPreference {
    if (canvasViewportSeq(remote) > canvasViewportSeq(local)) {
        return { viewport: remote.viewport, viewportSeq: remote.viewportSeq };
    }
    return {
        viewport: local.viewport || remote.viewport,
        viewportSeq: local.viewportSeq ?? remote.viewportSeq,
    };
}

/** 打开中的编辑器只在视口序号前进时改相机。 */
export function incomingCanvasViewport(currentSeq: number, project: ViewportPreference | null | undefined): { seq: number; viewport: ViewportTransform } | null {
    if (!project?.viewport) return null;
    const seq = canvasViewportSeq(project);
    if (seq <= currentSeq) return null;
    return { seq, viewport: project.viewport };
}
