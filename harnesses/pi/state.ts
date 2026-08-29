import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { randomBytes } from "node:crypto";
import { resolve } from "node:path";

export const stateEntryType = "madeleine-state-v1";
const boundaryEntryType = "madeleine-boundary-v1";

export interface PersistedState {
  version: 1;
  conversation_id: string;
  capture_id: string;
  injected_paths: string[];
}

export interface ConversationIdentity {
  externalID: string;
  transcriptRef: string;
}

export class PiState {
  private conversationID = "";
  private captureID: string | undefined;
  private readonly injectedPaths = new Set<string>();
  private readonly inFlightPaths = new Set<string>();

  constructor(
    private readonly pi: ExtensionAPI,
    private readonly generateID: () => string = uuidv7,
  ) {}

  initialize(ctx: ExtensionContext, reason: "startup" | "reload" | "new" | "resume" | "fork"): ConversationIdentity {
    const sessionFile = ctx.sessionManager.getSessionFile();
    const persistedExternalID = sessionFile ? resolve(sessionFile) : undefined;
    const restored = reason === "reload" ? newestState(ctx, persistedExternalID) : undefined;

    this.conversationID = persistedExternalID ?? restored?.conversation_id ?? this.generateID();
    this.captureID = restored?.capture_id;
    this.injectedPaths.clear();
    for (const path of restored?.injected_paths ?? []) this.injectedPaths.add(path);
    this.inFlightPaths.clear();

    return {
      externalID: this.conversationID,
      transcriptRef: persistedExternalID ?? "",
    };
  }

  currentCaptureID(): string | undefined {
    return this.captureID;
  }

  attachCapture(captureID: string): void {
    this.captureID = captureID;
    this.injectedPaths.clear();
    this.inFlightPaths.clear();
    this.save();
  }

  clearCapture(): void {
    this.captureID = undefined;
    this.injectedPaths.clear();
    this.inFlightPaths.clear();
  }

  claimPath(path: string): boolean {
    if (this.injectedPaths.has(path) || this.inFlightPaths.has(path)) return false;
    this.inFlightPaths.add(path);
    return true;
  }

  releasePath(path: string): void {
    this.inFlightPaths.delete(path);
  }

  recordInjectedPath(path: string): void {
    this.inFlightPaths.delete(path);
    this.injectedPaths.add(path);
    this.save();
  }

  ensureCursor(ctx: ExtensionContext): string {
    const cursor = ctx.sessionManager.getLeafId();
    if (cursor) return cursor;

    this.pi.appendEntry(boundaryEntryType, { version: 1 });
    const boundaryCursor = ctx.sessionManager.getLeafId();
    if (!boundaryCursor) throw new Error("Pi did not persist the Madeleine boundary entry");
    return boundaryCursor;
  }

  private save(): void {
    if (!this.captureID) return;
    this.pi.appendEntry<PersistedState>(stateEntryType, {
      version: 1,
      conversation_id: this.conversationID,
      capture_id: this.captureID,
      injected_paths: [...this.injectedPaths].sort(),
    });
  }
}

function newestState(ctx: ExtensionContext, conversationID?: string): PersistedState | undefined {
  const entries = ctx.sessionManager.getBranch();
  for (let index = entries.length - 1; index >= 0; index--) {
    const entry = entries[index];
    if (entry.type !== "custom" || entry.customType !== stateEntryType) continue;
    if (!isPersistedState(entry.data)) continue;
    if (conversationID && entry.data.conversation_id !== conversationID) continue;
    return {
      ...entry.data,
      injected_paths: [...new Set(entry.data.injected_paths)].sort(),
    };
  }
  return undefined;
}

function isPersistedState(value: unknown): value is PersistedState {
  if (!isObject(value)) return false;
  return (
    value.version === 1 &&
    typeof value.conversation_id === "string" &&
    value.conversation_id.length > 0 &&
    typeof value.capture_id === "string" &&
    value.capture_id.length > 0 &&
    Array.isArray(value.injected_paths) &&
    value.injected_paths.every((path) => typeof path === "string" && path.length > 0)
  );
}

function uuidv7(): string {
  const bytes = randomBytes(16);
  const timestamp = Date.now();
  bytes[0] = Math.floor(timestamp / 2 ** 40) & 0xff;
  bytes[1] = Math.floor(timestamp / 2 ** 32) & 0xff;
  bytes[2] = Math.floor(timestamp / 2 ** 24) & 0xff;
  bytes[3] = Math.floor(timestamp / 2 ** 16) & 0xff;
  bytes[4] = Math.floor(timestamp / 2 ** 8) & 0xff;
  bytes[5] = timestamp & 0xff;
  bytes[6] = (bytes[6]! & 0x0f) | 0x70;
  bytes[8] = (bytes[8]! & 0x3f) | 0x80;

  const hex = bytes.toString("hex");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
