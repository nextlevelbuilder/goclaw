import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { TeamTaskData } from "@/types/team";

const events = vi.hoisted(() => new Map<string, (payload: unknown) => void>());
vi.mock("@/hooks/use-ws-event", () => ({
  useWsEvent: (event: string, handler: (payload: unknown) => void) => { events.set(event, handler); },
}));

import { useBoardTasks } from "./use-board-tasks";

const task = { id: "task-1", team_id: "team-1", subject: "Task", status: "blocked" } as TeamTaskData;

afterEach(() => {
  cleanup();
  events.clear();
  vi.useRealTimers();
});

describe("useBoardTasks workflow event refetching", () => {
  it("debounces matching team.workflow.updated events into one authoritative board refetch", async () => {
    vi.useFakeTimers();
    const getTeamTasks = vi.fn().mockResolvedValue({ tasks: [task], count: 1 });
    const getTaskLight = vi.fn().mockResolvedValue(task);
    renderHook(() => useBoardTasks({ teamId: "team-1", getTeamTasks, getTaskLight, statusFilter: "all", selectedScope: null }));

    act(() => {
      events.get("team.workflow.updated")?.({ team_id: "other-team", workflow_id: "workflow-1" });
      events.get("team.workflow.updated")?.({ team_id: "team-1", workflow_id: "workflow-1" });
      events.get("team.workflow.updated")?.({ team_id: "team-1", workflow_id: "workflow-1" });
      vi.advanceTimersByTime(299);
    });
    expect(getTeamTasks).not.toHaveBeenCalled();
    await act(async () => { vi.advanceTimersByTime(1); await Promise.resolve(); });
    expect(getTeamTasks).toHaveBeenCalledTimes(1);
    expect(getTeamTasks).toHaveBeenCalledWith("team-1", "all", undefined, undefined);
  });

  it("debounces matching team.task.blocked events into one exact task refetch", async () => {
    vi.useFakeTimers();
    const getTeamTasks = vi.fn().mockResolvedValue({ tasks: [], count: 0 });
    const getTaskLight = vi.fn().mockResolvedValue(task);
    renderHook(() => useBoardTasks({ teamId: "team-1", getTeamTasks, getTaskLight, statusFilter: "all", selectedScope: null }));

    act(() => {
      events.get("team.task.blocked")?.({ team_id: "other-team", task_id: "task-1" });
      events.get("team.task.blocked")?.({ team_id: "team-1", task_id: "task-1" });
      events.get("team.task.blocked")?.({ team_id: "team-1", task_id: "task-1" });
      vi.advanceTimersByTime(299);
    });
    expect(getTaskLight).not.toHaveBeenCalled();
    await act(async () => { vi.advanceTimersByTime(1); await Promise.resolve(); });
    expect(getTaskLight).toHaveBeenCalledOnce();
    expect(getTaskLight).toHaveBeenCalledWith("team-1", "task-1");
  });
});
