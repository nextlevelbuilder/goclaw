import { cleanup, render, screen } from "@testing-library/react";
import { I18nextProvider } from "react-i18next";
import { afterEach, describe, expect, it, vi } from "vitest";
import i18n from "@/i18n";
import type { TeamTaskData } from "@/types/team";

// framer-motion's layout animations are irrelevant here; render a plain div.
vi.mock("framer-motion", () => ({
  motion: new Proxy({}, {
    get: () => ({ children, ...props }: { children?: React.ReactNode } & Record<string, unknown>) => {
      // Strip framer-only props so React does not warn about unknown DOM attrs.
      const rest = { ...props };
      for (const key of ["layoutId", "layout", "initial", "transition"]) delete rest[key];
      return <div {...rest}>{children}</div>;
    },
  }),
}));
vi.mock("@/components/ui/badge", () => ({
  Badge: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}));

import { KanbanCard } from "./kanban-card";

const task = {
  id: "task-1", team_id: "team-1", subject: "Blocked work", status: "blocked",
  priority: 1, workflow_id: "workflow-1", workflow_step_id: "step-2", plan_revision: 4,
} as TeamTaskData;

afterEach(() => { cleanup(); });

describe("KanbanCard workflow badges", () => {
  it("renders the step+revision badge and a translated status badge for workflow tasks", () => {
    render(
      <I18nextProvider i18n={i18n}>
        <KanbanCard task={task} onClick={vi.fn()} />
      </I18nextProvider>,
    );
    // Combined step + revision badge.
    expect(screen.getByText(/step-2/)).toBeInTheDocument();
    expect(screen.getByText(/r4/)).toBeInTheDocument();
    // Translated task-status badge.
    expect(screen.getByText("Blocked")).toBeInTheDocument();
  });

  it("omits the status badge when the task has no workflow", () => {
    render(
      <I18nextProvider i18n={i18n}>
        <KanbanCard task={{ ...task, workflow_id: undefined, workflow_step_id: undefined }} onClick={vi.fn()} />
      </I18nextProvider>,
    );
    expect(screen.queryByText("Blocked")).not.toBeInTheDocument();
  });
});
