import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

import { consumeDirectorPreviewDispatches, isDirectorPreviewDispatch, runDirectorPreviewNodes, type DirectorPreviewDispatchItem } from "@/pages/canvas/director-preview-runner";

const dispatch: DirectorPreviewDispatchItem = {
    operationId: "confirm:gp-1",
    proposalId: "gp-1",
    canvasId: "canvas-1",
    nodeIds: ["dir-1"],
    kind: "director_preview",
    modelKey: "local::director-clay-preview",
    charged: false,
};

describe("白膜确认在桌面走导演台录制", () => {
    test("导演台准备好后只打开并导出一次", async () => {
        const opened: string[] = [];
        const exported: string[] = [];
        let ready = false;
        await runDirectorPreviewNodes({
            nodeIds: [" dir-1 ", "dir-1"],
            openDirector: (nodeId) => {
                opened.push(nodeId);
                ready = true;
                return true;
            },
            exportFor: (nodeId) => (ready ? async () => { exported.push(nodeId); } : null),
            wait: async () => {},
            now: () => 0,
        });
        expect(opened).toEqual(["dir-1"]);
        expect(exported).toEqual(["dir-1"]);
    });

    test("不是导演台时不开始录制", async () => {
        let exports = 0;
        await expect(runDirectorPreviewNodes({
            nodeIds: ["video-1"],
            openDirector: () => false,
            exportFor: () => { exports += 1; return async () => {}; },
            wait: async () => {},
        })).rejects.toThrow("不是导演台");
        expect(exports).toBe(0);
    });

    test("导演台迟迟没准备好就停下", async () => {
        let clock = 0;
        await expect(runDirectorPreviewNodes({
            nodeIds: ["dir-1"],
            openDirector: () => true,
            exportFor: () => null,
            wait: async () => {},
            timeoutMs: 30,
            now: () => { clock += 20; return clock; },
        })).rejects.toThrow("导演台还没准备好");
    });
});

describe("桌面只消费免费白膜确认", () => {
    test("已开始的确认再次出现时只补 ack", async () => {
        const started = new Set<string>();
        const acked: string[] = [];
        const runs: string[][] = [];
        const deps = {
            started,
            run: async (nodeIds: string[]) => { runs.push(nodeIds); },
            ack: async (item: DirectorPreviewDispatchItem) => { acked.push(item.operationId); },
        };
        await consumeDirectorPreviewDispatches([dispatch], deps);
        await consumeDirectorPreviewDispatches([dispatch], { ...deps, run: async () => { throw new Error("second"); } });
        expect(runs).toEqual([["dir-1"]]);
        expect(acked).toEqual(["confirm:gp-1", "confirm:gp-1"]);
    });

    test("录制失败会放开认领，计费或错误模型直接跳过", async () => {
        const started = new Set<string>();
        await expect(consumeDirectorPreviewDispatches([dispatch], {
            started,
            run: async () => { throw new Error("录制失败"); },
            ack: async () => { throw new Error("不应 ack"); },
        })).rejects.toThrow("录制失败");
        expect(started.size).toBe(0);
        expect(isDirectorPreviewDispatch({ ...dispatch, charged: true })).toBe(false);
        expect(isDirectorPreviewDispatch({ ...dispatch, modelKey: "beefapi::video" })).toBe(false);
        const acked: string[] = [];
        await consumeDirectorPreviewDispatches([{ ...dispatch, charged: true }, { ...dispatch, modelKey: "other" }], {
            started,
            run: async () => { throw new Error("不应录制"); },
            ack: async (item) => { acked.push(item.operationId); },
        });
        expect(acked).toEqual([]);
    });
});

test("画布页把 CLI 确认和「导出白膜」接到同一条录制", () => {
    const project = readFileSync(new URL("../src/pages/canvas/project.tsx", import.meta.url), "utf8");
    const workbench = readFileSync(new URL("../src/components/canvas/director/canvas-director-workbench.tsx", import.meta.url), "utf8");
    expect(project).toContain("useDirectorPreviewDispatch");
    expect(project).toContain("runDirectorPreviewNodes");
    expect(project).toContain('proposal.kind === "director_preview"');
    expect(workbench).toContain("onExportClayReady");
    expect(workbench).toContain("exportClayVideoRef");
});
