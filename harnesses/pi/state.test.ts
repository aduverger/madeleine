import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { PiState, stateEntryType, type PersistedState } from "./state.ts";

interface CustomEntry {
  type: "custom";
  id: string;
  parentId: string | null;
  customType: string;
  data: unknown;
}

function harness(sessionFile?: string, initialEntries: CustomEntry[] = []) {
  const entries = [...initialEntries];
  let leaf = entries.at(-1)?.id ?? null;
  const pi = {
    appendEntry(customType: string, data: unknown) {
      const id = `entry-${entries.length + 1}`;
      entries.push({ type: "custom", id, parentId: leaf, customType, data });
      leaf = id;
    },
  } as unknown as ExtensionAPI;
  const ctx = {
    sessionManager: {
      getSessionFile: () => sessionFile,
      getLeafId: () => leaf,
      getBranch: () => entries,
    },
  } as unknown as ExtensionContext;
  return { pi, ctx, entries };
}

function entry(id: string, data: unknown): CustomEntry {
  return { type: "custom", id, parentId: null, customType: stateEntryType, data };
}

describe("PiState", () => {
  it("uses the cleaned session path for persisted Conversation identity", () => {
    const { pi, ctx } = harness("relative/session.jsonl");
    const state = new PiState(pi);

    expect(state.initialize(ctx, "startup")).toEqual({
      externalID: resolve("relative/session.jsonl"),
      transcriptRef: resolve("relative/session.jsonl"),
    });
  });

  it("restores only the newest valid state for the current Conversation", () => {
    const sessionFile = resolve("session.jsonl");
    const older: PersistedState = {
      version: 1,
      conversation_id: sessionFile,
      capture_id: "capture-old",
      injected_paths: ["b.ts"],
    };
    const newest: PersistedState = {
      version: 1,
      conversation_id: sessionFile,
      capture_id: "capture-new",
      injected_paths: ["b.ts", "a.ts", "a.ts"],
    };
    const { pi, ctx, entries } = harness(sessionFile, [
      entry("one", older),
      entry("two", { version: 2 }),
      entry("three", { ...newest, conversation_id: "another-session" }),
      entry("four", newest),
    ]);
    const state = new PiState(pi);

    state.initialize(ctx, "startup");
    expect(state.currentCaptureID()).toBe("capture-new");
    expect(state.claimPath("a.ts")).toBe(false);
    expect(state.claimPath("new.ts")).toBe(true);
    state.recordInjectedPath("new.ts");

    expect(entries.at(-1)?.data).toEqual({
      ...newest,
      injected_paths: ["a.ts", "b.ts", "new.ts"],
    });
  });

  it("does not inherit Capture state into a new or forked Conversation", () => {
    const sessionFile = resolve("session.jsonl");
    const persisted: PersistedState = {
      version: 1,
      conversation_id: sessionFile,
      capture_id: "capture-old",
      injected_paths: ["a.ts"],
    };

    for (const reason of ["new", "fork"] as const) {
      const { pi, ctx } = harness(sessionFile, [entry("one", persisted)]);
      const state = new PiState(pi);
      state.initialize(ctx, reason);
      expect(state.currentCaptureID()).toBeUndefined();
      expect(state.claimPath("a.ts")).toBe(true);
    }
  });

  it("keeps an ephemeral Conversation across reload but not process death", () => {
    const { pi, ctx, entries } = harness();
    const first = new PiState(pi, () => "018f0000-0000-7000-8000-000000000001");
    const identity = first.initialize(ctx, "startup");
    first.attachCapture("capture-1");

    const reloaded = new PiState(pi, () => "should-not-be-used");
    expect(reloaded.initialize(ctx, "reload")).toEqual(identity);
    expect(reloaded.currentCaptureID()).toBe("capture-1");

    const freshHarness = harness();
    const fresh = new PiState(freshHarness.pi, () => "018f0000-0000-7000-8000-000000000002");
    expect(fresh.initialize(freshHarness.ctx, "startup").externalID).not.toBe(identity.externalID);
    expect(entries).toHaveLength(1);
  });

  it("generates a UUIDv7 for an ephemeral session", () => {
    const { pi, ctx } = harness();
    const identity = new PiState(pi).initialize(ctx, "startup");
    expect(identity.externalID).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
    expect(identity.transcriptRef).toBe("");
  });

  it("creates a real Pi entry when an empty session has no leaf cursor", () => {
    const { pi, ctx, entries } = harness("/sessions/empty.jsonl");
    const state = new PiState(pi);
    state.initialize(ctx, "startup");

    expect(state.ensureCursor(ctx)).toBe("entry-1");
    expect(entries[0]).toMatchObject({ customType: "madeleine-boundary-v1" });
    expect(state.ensureCursor(ctx)).toBe("entry-1");
  });
});
