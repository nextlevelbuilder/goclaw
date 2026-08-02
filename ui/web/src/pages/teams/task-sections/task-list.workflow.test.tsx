import { cleanup, render, screen } from "@testing-library/react";
import { I18nextProvider } from "react-i18next";
import { afterEach, describe, expect, it, vi } from "vitest";
import i18n from "@/i18n";
import type { TeamTaskData } from "@/types/team";

vi.mock("@/components/ui/badge", () => ({
  Badge: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}));
vi.mock("@/components/ui/button", () => ({
  Button: ({ children }: { children: React.ReactNode }) => <button>{children}</button>,
}));
vi.mock("@/components/shared/confirm-dialog", () => ({ ConfirmDialog: () => null }));
vi.mock("@/components/shared/confirm-delete-dialog", () => ({ ConfirmDeleteDialog: () => null }));
vi.mock("@/components/shared/pagination", () => ({ Pagination: () => null }));

import { TaskList } from "./task-list";

const task = {
  id: "task-1", team_id: "team-1", subject: "Blocked work", status: "blocked",
  priority: 1, workflow_id: "workflow-1", workflow_step_id: "step-2", plan_revision: 4,
} as TeamTaskData;

afterEach(() => { cleanup(); });

describe("TaskList workflow badges", () => {
  it("renders the step+revision badge and a translated status badge for workflow tasks", () => {
    render(
      <I18nextProvider i18n={i18n}>
        <TaskList
          tasks={[task]}
          loading={false}
          teamId="team-1"
          members={[]}
          getTaskDetail={vi.fn()}
          getWorkflow={vi.fn()}
          applyWorkflowAction={vi.fn()}
        />
      </I18nextProvider>,
    );
    // Combined step + revision badge.
    expect(screen.getByText(/step-2/)).toBeInTheDocument();
    expect(screen.getByText(/r4/)).toBeInTheDocument();
    // Translated task-status badge (the list always renders one).
    expect(screen.getByText("Blocked")).toBeInTheDocument();
  });
});
