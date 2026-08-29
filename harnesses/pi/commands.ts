import type {
  ExtensionAPI,
  ExtensionCommandContext,
} from "@earendil-works/pi-coding-agent";

import type { Capture, DoctorCheck } from "./rpc.ts";

const usage = "Usage: /madeleine status | abandon <capture-id> | doctor";

interface CommandClient {
  doctor(repositoryRoot: string): Promise<DoctorCheck[]>;
  listPendingCaptures(repositoryRoot: string, externalID?: string): Promise<Capture[]>;
  abandonCapture(captureID: string): Promise<void>;
}

interface CurrentCapture {
  currentCaptureID(): string | undefined;
  clearCurrentCapture(captureID: string): void;
}

export function registerCommands(
  pi: ExtensionAPI,
  client: CommandClient,
  current: CurrentCapture,
): void {
  pi.registerCommand("madeleine", {
    description: "Show Madeleine status, abandon a Capture, or run doctor checks",
    handler: async (argumentsText, ctx) => {
      const argumentsList = argumentsText.trim().split(/\s+/).filter(Boolean);
      switch (argumentsList[0]) {
        case "status":
          if (argumentsList.length !== 1) return showUsage(ctx);
          return showStatus(client, current.currentCaptureID(), ctx);
        case "abandon":
          if (argumentsList.length !== 2) return showUsage(ctx);
          return abandon(client, current, argumentsList[1]!, ctx);
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

async function abandon(
  client: CommandClient,
  current: CurrentCapture,
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
    current.clearCurrentCapture(captureID);
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
