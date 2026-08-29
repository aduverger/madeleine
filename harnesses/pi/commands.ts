import type {
  ExtensionAPI,
  ExtensionCommandContext,
} from "@earendil-works/pi-coding-agent";

import type { RolloverResult } from "./lifecycle.ts";
import type { Capture, DoctorCheck } from "./rpc.ts";

const usage = "Usage: /madeleine status | rollover | abandon <capture-id> | doctor";

interface CommandClient {
  doctor(repositoryRoot: string): Promise<DoctorCheck[]>;
  listPendingCaptures(repositoryRoot: string, externalID?: string): Promise<Capture[]>;
  abandonCapture(captureID: string): Promise<void>;
}

interface CaptureController {
  currentCaptureID(): string | undefined;
  clearCurrentCapture(captureID: string): void;
  rollover(ctx: ExtensionCommandContext): Promise<RolloverResult>;
}

export function registerCommands(
  pi: ExtensionAPI,
  client: CommandClient,
  captureController: CaptureController,
): void {
  pi.registerCommand("madeleine", {
    description: "Show status, roll over or abandon a Capture, or run doctor checks",
    handler: async (argumentsText, ctx) => {
      const argumentsList = argumentsText.trim().split(/\s+/).filter(Boolean);
      switch (argumentsList[0]) {
        case "status":
          if (argumentsList.length !== 1) return showUsage(ctx);
          return showStatus(client, captureController.currentCaptureID(), ctx);
        case "rollover":
          if (argumentsList.length !== 1) return showUsage(ctx);
          return rollover(captureController, ctx);
        case "abandon":
          if (argumentsList.length !== 2) return showUsage(ctx);
          return abandon(client, captureController, argumentsList[1]!, ctx);
        case "doctor":
          if (argumentsList.length !== 1) return showUsage(ctx);
          return showDoctor(client, ctx);
        default:
          return showUsage(ctx);
      }
    },
  });
}

async function showStatus(
  client: CommandClient,
  currentCaptureID: string | undefined,
  ctx: ExtensionCommandContext,
): Promise<void> {
  try {
    const pendingCaptures = (await client.listPendingCaptures(ctx.cwd)).filter(isPendingCapture);
    if (pendingCaptures.length === 0) {
      ctx.ui.notify("Madeleine has no open or pending Captures in this repository.", "info");
      return;
    }
    const lines = pendingCaptures.map((capture) => {
      const marker = capture.id === currentCaptureID ? "*" : " ";
      return `${marker} ${capture.id}  ${capture.status}  ${capture.started_at}`;
    });
    ctx.ui.notify(["Madeleine Captures (* current):", ...lines].join("\n"), "info");
  } catch {
    ctx.ui.notify("Madeleine status is unavailable.", "error");
  }
}

async function rollover(
  captureController: CaptureController,
  ctx: ExtensionCommandContext,
): Promise<void> {
  try {
    await ctx.waitForIdle();
    const result = await captureController.rollover(ctx);
    const outcome = result.sealed.empty
      ? `Abandoned empty Capture ${result.sealed.capture_id}.`
      : `Sealed Capture ${result.sealed.capture_id}; Episode publication is pending.`;
    ctx.ui.notify(`${outcome}\nStarted Capture ${result.startedCaptureID}.`, "info");
  } catch {
    ctx.ui.notify("Madeleine could not roll over the current Capture.", "error");
  }
}

async function abandon(
  client: CommandClient,
  captureController: CaptureController,
  captureID: string,
  ctx: ExtensionCommandContext,
): Promise<void> {
  try {
    const pendingCaptures = await client.listPendingCaptures(ctx.cwd);
    const canAbandon = pendingCaptures.some(
      (capture) => capture.id === captureID && isPendingCapture(capture),
    );
    if (!canAbandon) {
      ctx.ui.notify("That Capture is not open or pending in this repository.", "error");
      return;
    }
    if (!ctx.hasUI) {
      ctx.ui.notify("Capture abandonment requires interactive confirmation.", "error");
      return;
    }
    const confirmed = await ctx.ui.confirm(
      "Abandon Madeleine Capture?",
      `Delete unfinished data for ${captureID}?`,
    );
    if (!confirmed) return;

    await client.abandonCapture(captureID);
    captureController.clearCurrentCapture(captureID);
    ctx.ui.notify(`Abandoned Madeleine Capture ${captureID}.`, "info");
  } catch {
    ctx.ui.notify("Madeleine could not abandon that Capture.", "error");
  }
}

async function showDoctor(client: CommandClient, ctx: ExtensionCommandContext): Promise<void> {
  try {
    const checks = await client.doctor(ctx.cwd);
    const lines = checks.map(
      (check) => `${check.ok ? "ok" : "failed"}: ${check.name} — ${check.detail}`,
    );
    ctx.ui.notify(lines.join("\n"), checks.every((check) => check.ok) ? "info" : "warning");
  } catch {
    ctx.ui.notify("Madeleine doctor is unavailable.", "error");
  }
}

function isPendingCapture(capture: Capture): boolean {
  return capture.status === "open" || capture.status === "pending_summary";
}

function showUsage(ctx: ExtensionCommandContext): void {
  ctx.ui.notify(usage, "info");
}
