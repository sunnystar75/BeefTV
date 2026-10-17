import type { DirectorPreviewDispatchItem } from "@/pages/canvas/director-preview-runner";

import { http } from "./request";

export async function waitDirectorPreviewDispatches(canvasId: string, signal?: AbortSignal): Promise<DirectorPreviewDispatchItem[]> {
    const data = await http.get<{ dispatches?: DirectorPreviewDispatchItem[] }>(
        `/desktop/director-preview-dispatches?canvasId=${encodeURIComponent(canvasId)}&waitMs=2500`,
        { signal, timeout: 4_000 },
    );
    return Array.isArray(data?.dispatches) ? data.dispatches : [];
}

export async function ackDirectorPreviewDispatch(canvasId: string, operationId: string) {
    await http.post<{ acked: boolean }>("/desktop/director-preview-dispatches/ack", { canvasId, operationId });
}
