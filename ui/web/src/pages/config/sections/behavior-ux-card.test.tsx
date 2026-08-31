// fireEvent (not userEvent): @testing-library/user-event is not in the frozen
// lockfile, and adding it would modify pnpm-lock.yaml. A click on the Radix
// switch is enough to prove the handler is wired and the control is interactive.
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nextProvider } from "react-i18next";
import { afterEach, describe, expect, it, vi } from "vitest";
import i18n from "@/i18n";
import { BehaviorUxCard } from "./behavior-ux-card";

afterEach(() => { cleanup(); });

// The Team Work switch is the second switch in the card (Intent Classify is first).
const teamWorkSwitch = () => screen.getAllByRole("switch")[1]!;

function renderCard(teamWork: boolean, onChange = vi.fn()) {
  render(
    <I18nextProvider i18n={i18n}>
      <BehaviorUxCard
        value={{ intent_classify: false, team_work_classify: teamWork }}
        onChange={onChange}
      />
    </I18nextProvider>,
  );
  return onChange;
}

describe("BehaviorUxCard Team Work switch", () => {
  it("reflects value.team_work_classify=true with no embedding provider configured", () => {
    renderCard(true);
    expect(teamWorkSwitch()).toHaveAttribute("data-state", "checked");
    expect(teamWorkSwitch()).not.toBeDisabled();
  });

  it("reflects value.team_work_classify=false and stays interactive", () => {
    renderCard(false);
    expect(teamWorkSwitch()).toHaveAttribute("data-state", "unchecked");
    expect(teamWorkSwitch()).not.toBeDisabled();
  });

  // Regression: the switch used to be force-unchecked and disabled whenever no
  // embedding provider existed (checked: value.team_work_classify && embeddingConfigured
  // / disabled: !embeddingConfigured). Team Work routing is a single LLM call and
  // consumes no embedder, so the card must render no embedding-required gate.
  it("renders no embedding-required disabled hint", () => {
    renderCard(false);
    expect(screen.queryByText(/embedding/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Configure embeddings/i)).not.toBeInTheDocument();
    for (const sw of screen.getAllByRole("switch")) expect(sw).not.toBeDisabled();
  });

  it("propagates a click to onChange when turning Team Work on", () => {
    const onChange = renderCard(false);
    fireEvent.click(teamWorkSwitch());
    expect(onChange).toHaveBeenCalledWith({ intent_classify: false, team_work_classify: true });
  });

  it("propagates a click to onChange when turning Team Work off", () => {
    const onChange = renderCard(true);
    fireEvent.click(teamWorkSwitch());
    expect(onChange).toHaveBeenCalledWith({ intent_classify: false, team_work_classify: false });
  });
});
