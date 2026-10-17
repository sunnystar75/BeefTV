import { useEffect, useRef } from "react";

import { ackDirectorPreviewDispatch, waitDirectorPreviewDispatches } from "@/services/api/director-preview-dispatch";

import { consumeDirectorPreviewDispatches } from "./director-preview-runner";

/** 画布打开时领取 CLI 确认的白膜导出，并走和点击「导出白膜」相同的录制。 */
export function useDirectorPreviewDispatch(canvasId: string, enabled: boolean, run: (nodeIds: string[]) => Promise<void>) {
    const runRef = useRef(run);
    runRef.current = run;
    const startedRef = useRef(new Set<string>());

    useEffect(() => {
        if (!enabled || !canvasId) return undefined;
        const controller = new AbortController();
        let timer = 0;
        const tick = async () => {
            if (controller.signal.aborted) return;
            try {
                const items = await waitDirectorPreviewDispatches(canvasId, controller.signal);
                if (controller.signal.aborted) return;
                await consumeDirectorPreviewDispatches(items, {
                    started: startedRef.current,
                    run: (nodeIds) => runRef.current(nodeIds),
                    ack: (item) => ackDirectorPreviewDispatch(item.canvasId || canvasId, item.operationId),
                });
            } catch {
                if (controller.signal.aborted) return;
            }
            if (!controller.signal.aborted) timer = window.setTimeout(() => void tick(), 200);
        };
        void tick();
        return () => {
            controller.abort();
            window.clearTimeout(timer);
        };
    }, [canvasId, enabled]);
}
