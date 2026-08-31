import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nextProvider } from "react-i18next";
import { afterEach, describe, expect, it, vi } from "vitest";
import i18n from "@/i18n";
import type { TeamTaskData, TeamWorkflowDetailResponse } from "@/types/team";

const events = vi.hoisted(() => new Map<string, (payload: unknown) => void>());
const toast = vi.hoisted(() => ({ success: vi.fn(), warning: vi.fn(), error: vi.fn() }));

vi.mock("@/hooks/use-ws-event", () => ({
  useWsEvent: (event: string, handler: (payload: unknown) => void) => { events.set(event, handler); },
}));
vi.mock("@/stores/use-toast-store", () => ({ toast }));
vi.mock("@/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: React.ReactNode }) => <h2>{children}</h2>,
}));
vi.mock("@/components/ui/badge", () => ({ Badge: ({ children }: { children: React.ReactNode }) => <span>{children}</span> }));
vi.mock("@/components/ui/button", () => ({
  Button: ({ children, onClick, disabled }: { children: React.ReactNode; onClick?: () => void; disabled?: boolean }) => <button disabled={disabled} onClick={onClick}>{children}</button>,
}));
vi.mock("@/components/ui/separator", () => ({ Separator: () => <hr /> }));
vi.mock("@/components/shared/confirm-dialog", () => ({ ConfirmDialog: () => null }));
vi.mock("./task-detail-content", () => ({ TaskDetailContent: () => null }));
vi.mock("./task-detail-attachments", () => ({ TaskDetailAttachments: () => null }));
vi.mock("./task-detail-comments", () => ({ TaskDetailComments: () => null }));
vi.mock("./task-detail-timeline", () => ({ TaskDetailTimeline: () => null }));
vi.mock("./workflow-action-dialog", () => ({
  WorkflowActionDialog: ({ action, onConfirm }: { action: string | null; onConfirm: (reason: string) => void }) => action ? <button onClick={() => onConfirm("operator reason")}>confirm {action}</button> : null,
}));

import { TaskDetailDialog } from "./task-detail-dialog";

const task = {
  id: "task-1", team_id: "team-1", subject: "Blocked work", status: "blocked",
  workflow_id: "workflow-1", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
} as TeamTaskData;

function workflow(status = "running", outcome?: "applied" | "already_applied" | "conflict"): TeamWorkflowDetailResponse & { outcome?: typeof outcome } {
  return {
    workflow: {
      id: "workflow-1", team_id: "team-1", status, plan_revision: 7,
      coordinator_agent_key: "coordinator", delivery_status: "dead",
      expansion_attempt_count: 2, delivery_attempt_count: 3,
      last_expansion_error: "expansion exploded", last_delivery_error: "delivery bounced",
      created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
    },
    tasks: [{
      id: "blocked-step", subject: "Repair", status: "blocked", workflow_step_id: "step-1",
      workflow_kind: "work", workflow_terminal: false, plan_revision: 7,
      blocker_reason: "waiting on upstream fix",
      recovery_count: 0, dispatch_count: 0, progress_percent: 0,
      created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
    }],
    allowed_actions: ["retry_blocked"],
    ...(outcome ? { action: "retry_blocked" as const, outcome } : {}),
  };
}

function renderDialog(
  overrides: Partial<React.ComponentProps<typeof TaskDetailDialog>> = {},
  postAction: TeamWorkflowDetailResponse & { outcome?: "applied" | "already_applied" | "conflict" } = workflow("needs_revision", "applied"),
) {
  // Initial mount load resolves "running"; the post-action reconciliation
  // refetch resolves the authoritative post-action state so both the action
  // response and the refetch agree.
  const getWorkflow = vi.fn()
    .mockResolvedValueOnce(workflow())
    .mockResolvedValue(postAction);
  const applyWorkflowAction = vi.fn().mockResolvedValue(postAction);
  const onWorkflowChanged = vi.fn();
  const props: React.ComponentProps<typeof TaskDetailDialog> = {
    task, teamId: "team-1", onClose: vi.fn(), getTaskDetail: vi.fn().mockResolvedValue({ task, comments: [], events: [], attachments: [] }),
    getWorkflow, applyWorkflowAction, onWorkflowChanged, ...overrides,
  };
  render(<I18nextProvider i18n={i18n}><TaskDetailDialog {...props} /></I18nextProvider>);
  return { getWorkflow, applyWorkflowAction, onWorkflowChanged };
}

afterEach(() => {
  cleanup();
  events.clear();
  vi.clearAllMocks();
  vi.useRealTimers();
});

describe("TaskDetailDialog workflow actions", () => {
  it("shows only server-authorized actions and sends the exact guarded payload without token fields", async () => {
    const { applyWorkflowAction } = renderDialog();
    fireEvent.click(await screen.findByRole("button", { name: "Retry blocked step" }));
    fireEvent.click(screen.getByRole("button", { name: "confirm retry_blocked" }));

    await waitFor(() => expect(applyWorkflowAction).toHaveBeenCalledOnce());
    expect(applyWorkflowAction).toHaveBeenCalledWith({
      teamId: "team-1", workflowId: "workflow-1", action: "retry_blocked",
      expectedStatus: "running", expectedPlanRevision: 7,
      taskId: "blocked-step", expectedTaskStatus: "blocked", reason: "operator reason",
    });
    const payload = applyWorkflowAction.mock.calls[0]![0] as Record<string, unknown>;
    expect(payload).not.toHaveProperty("token");
    expect(payload).not.toHaveProperty("expectedToken");
    expect(await screen.findByText("Needs revision")).toBeInTheDocument();
  });

  it.each(["applied", "already_applied", "conflict"] as const)("replaces the detail with authoritative %s action response", async (outcome) => {
    const { applyWorkflowAction } = renderDialog({}, workflow("completed", outcome));
    fireEvent.click(await screen.findByRole("button", { name: "Retry blocked step" }));
    fireEvent.click(screen.getByRole("button", { name: "confirm retry_blocked" }));
    await waitFor(() => expect(applyWorkflowAction).toHaveBeenCalledOnce());
    expect(await screen.findByText("Completed")).toBeInTheDocument();
  });

  it.each(["applied", "already_applied", "conflict"] as const)("reconciles %s by replacing detail, refetching workflow, and refetching the board", async (outcome) => {
    const { getWorkflow, applyWorkflowAction, onWorkflowChanged } = renderDialog({}, workflow("completed", outcome));
    // First getWorkflow call is the mount load.
    await waitFor(() => expect(getWorkflow).toHaveBeenCalledTimes(1));
    fireEvent.click(await screen.findByRole("button", { name: "Retry blocked step" }));
    fireEvent.click(screen.getByRole("button", { name: "confirm retry_blocked" }));

    // (1) authoritative action response replaces the local detail.
    await waitFor(() => expect(applyWorkflowAction).toHaveBeenCalledOnce());
    expect(await screen.findByText("Completed")).toBeInTheDocument();
    // (2) explicit workflow refetch fires (mount + reconciliation = 2).
    await waitFor(() => expect(getWorkflow).toHaveBeenCalledTimes(2));
    // (3) board refetch fires.
    await waitFor(() => expect(onWorkflowChanged).toHaveBeenCalledOnce());
  });

  it("renders distinct blocker, expansion, and delivery diagnostics as separate elements", async () => {
    renderDialog();
    expect(await screen.findByText(/waiting on upstream fix/)).toBeInTheDocument();
    expect(screen.getByText(/expansion exploded/)).toBeInTheDocument();
    expect(screen.getByText(/delivery bounced/)).toBeInTheDocument();
    // Distinct expansion / delivery attempt counters and dead delivery status.
    expect(screen.getByText("Expansion attempts: 2")).toBeInTheDocument();
    expect(screen.getByText("Delivery attempts: 3")).toBeInTheDocument();
    expect(screen.getByText("Delivery: Dead")).toBeInTheDocument();
    // Distinct labels prove the reasons are not collapsed into one fallback line.
    expect(screen.getByText("Blocker reason:")).toBeInTheDocument();
    expect(screen.getByText("Expansion error:")).toBeInTheDocument();
    expect(screen.getByText("Delivery error:")).toBeInTheDocument();
  });

  it("debounces matching workflow updates and refetches only authoritative workflow detail", async () => {
    vi.useFakeTimers();
    const { getWorkflow } = renderDialog();
    await act(async () => { await Promise.resolve(); });
    expect(getWorkflow).toHaveBeenCalledTimes(1);

    act(() => {
      events.get("team.workflow.updated")?.({ team_id: "other-team", workflow_id: "workflow-1" });
      events.get("team.workflow.updated")?.({ team_id: "team-1", workflow_id: "workflow-1" });
      events.get("team.workflow.updated")?.({ team_id: "team-1", workflow_id: "workflow-1" });
      vi.advanceTimersByTime(299);
    });
    expect(getWorkflow).toHaveBeenCalledTimes(1);
    await act(async () => { vi.advanceTimersByTime(1); await Promise.resolve(); });
    expect(getWorkflow).toHaveBeenCalledTimes(2);
    vi.useRealTimers();
  });
});
