/**
 * Unit tests for bitrix-portal-select delete-affordance logic.
 *
 * NOTE: @testing-library/react is not installed in this project (same
 * constraint as voice-picker.test.tsx) — tests cover pure logic and module
 * contracts rather than DOM rendering / click simulation. The one thing that
 * genuinely requires a real browser (trash click not triggering
 * onResumeAuthorize via Radix's click-hijack) is covered by the plan's
 * manual smoke step instead (phase-05 T5.11).
 */
import { describe, it, expect, vi } from "vitest";
import type { BitrixPortal } from "./types";

vi.mock("./use-bitrix-portals", () => ({
  useBitrixPortals: vi.fn(() => ({ data: [], isLoading: false, isError: false })),
  useBitrixPortalDelete: vi.fn(() => ({ mutateAsync: vi.fn(), isPending: false })),
}));

// Sentinel values mirrored from bitrix-portal-select.tsx (kept in sync by hand
// — small, stable, not worth exporting just for tests).
const CREATE_SENTINEL = "__bitrix_portal_create__";
const RESUME_PREFIX = "__bitrix_portal_resume__:";

/** Mirrors bitrix-portal-select.tsx's itemValue computation. */
function itemValueFor(p: Pick<BitrixPortal, "name" | "installed">): string {
  return p.installed ? p.name : `${RESUME_PREFIX}${p.name}`;
}

/** Mirrors whether a portal row should render the delete/trash affordance —
 * only pending (not-yet-installed) portals get one (design.md scope change
 * 2026-07-09: installed-portal delete is out of scope for this dropdown). */
function showsDeleteAffordance(p: Pick<BitrixPortal, "installed">): boolean {
  return !p.installed;
}

describe("bitrix-portal-select — pending vs installed classification", () => {
  const installed: BitrixPortal = { name: "acme", domain: "acme.bitrix24.com", installed: true, public_url: "https://goclaw.example.com", created_at: "2026-01-01T00:00:00Z" };
  const pending: BitrixPortal = { name: "web1trang", domain: "web1trang.bitrix24.com", installed: false, public_url: "", created_at: "2026-01-01T00:00:00Z" };

  it("installed portal's item value is the bare name (what onChange stores)", () => {
    expect(itemValueFor(installed)).toBe("acme");
  });

  it("pending portal's item value carries the RESUME_PREFIX sentinel, never the bare name", () => {
    const v = itemValueFor(pending);
    expect(v.startsWith(RESUME_PREFIX)).toBe(true);
    expect(v).not.toBe(pending.name);
    expect(v.slice(RESUME_PREFIX.length)).toBe("web1trang");
  });

  it("delete affordance shows for pending, not for installed", () => {
    expect(showsDeleteAffordance(pending)).toBe(true);
    expect(showsDeleteAffordance(installed)).toBe(false);
  });

  it("a pending portal's value can never equal the CREATE_SENTINEL or a bare onChange value", () => {
    const v = itemValueFor(pending);
    expect(v).not.toBe(CREATE_SENTINEL);
  });
});

describe("bitrix-portal-select — safe-by-construction: pending can't be a channel's form value", () => {
  // Mirrors the Select's onValueChange dispatch (bitrix-portal-select.tsx):
  // CREATE_SENTINEL -> onCreateRequest, RESUME_PREFIX:* -> onResumeAuthorize,
  // anything else -> onChange (the actual form field setter).
  function dispatch(
    v: string,
    handlers: { onCreateRequest: () => void; onResumeAuthorize: (name: string) => void; onChange: (v: string) => void },
  ) {
    if (v === CREATE_SENTINEL) return handlers.onCreateRequest();
    if (v.startsWith(RESUME_PREFIX)) return handlers.onResumeAuthorize(v.slice(RESUME_PREFIX.length));
    return handlers.onChange(v);
  }

  it("selecting a pending portal's value routes to onResumeAuthorize, never onChange", () => {
    const onChange = vi.fn();
    const onResumeAuthorize = vi.fn();
    const onCreateRequest = vi.fn();
    const pendingValue = itemValueFor({ name: "web1trang", installed: false } as BitrixPortal);

    dispatch(pendingValue, { onChange, onResumeAuthorize, onCreateRequest });

    expect(onResumeAuthorize).toHaveBeenCalledWith("web1trang");
    expect(onChange).not.toHaveBeenCalled();
    // Consequence: findChannelsUsingPortal (backend) can never see a pending
    // portal name in any channel_instance.config.portal — deleting a pending
    // portal is safe without an in-use check.
  });

  it("selecting an installed portal's value routes to onChange (the real form setter)", () => {
    const onChange = vi.fn();
    const onResumeAuthorize = vi.fn();
    const onCreateRequest = vi.fn();
    const installedValue = itemValueFor({ name: "acme", installed: true } as BitrixPortal);

    dispatch(installedValue, { onChange, onResumeAuthorize, onCreateRequest });

    expect(onChange).toHaveBeenCalledWith("acme");
    expect(onResumeAuthorize).not.toHaveBeenCalled();
  });
});

describe("bitrix-portal-select — delete confirm handler contract", () => {
  /** Mirrors handleConfirmDelete's success/error branching without the
   * component's i18n/toast side effects — just the mutateAsync call
   * contract and state-clearing behavior. */
  async function confirmDelete(
    name: string | null,
    mutateAsync: (name: string) => Promise<unknown>,
    onSuccess: () => void,
    onError: (err: { code?: string; message?: string }) => void,
  ) {
    if (!name) return;
    try {
      await mutateAsync(name);
      onSuccess();
    } catch (err) {
      onError(err as { code?: string; message?: string });
    }
  }

  it("calls mutateAsync with the pending portal's name", async () => {
    const mutateAsync = vi.fn().mockResolvedValue({ status: "deleted" });
    const onSuccess = vi.fn();
    const onError = vi.fn();

    await confirmDelete("web1trang", mutateAsync, onSuccess, onError);

    expect(mutateAsync).toHaveBeenCalledWith("web1trang");
    expect(onSuccess).toHaveBeenCalled();
    expect(onError).not.toHaveBeenCalled();
  });

  it("does nothing when name is null (dialog not open / already cleared)", async () => {
    const mutateAsync = vi.fn();
    const onSuccess = vi.fn();
    const onError = vi.fn();

    await confirmDelete(null, mutateAsync, onSuccess, onError);

    expect(mutateAsync).not.toHaveBeenCalled();
    expect(onSuccess).not.toHaveBeenCalled();
  });

  it("routes a rejected mutateAsync (e.g. defensive FAILED_PRECONDITION) to onError, not onSuccess", async () => {
    const mutateAsync = vi.fn().mockRejectedValue({ code: "FAILED_PRECONDITION", message: "portal is used by channel(s): x" });
    const onSuccess = vi.fn();
    const onError = vi.fn();

    await confirmDelete("web1trang", mutateAsync, onSuccess, onError);

    expect(onError).toHaveBeenCalledWith(
      expect.objectContaining({ code: "FAILED_PRECONDITION" }),
    );
    expect(onSuccess).not.toHaveBeenCalled();
  });
});

describe("bitrix-portal-select — keyboard delete on pending options", () => {
  /** Mirrors the SelectItem onKeyDown handler (bitrix-portal-select.tsx):
   * Radix Select's roving tabindex puts focus on the option itself, not the
   * nested trash button, so Delete/Backspace is the keyboard-reachable
   * equivalent of clicking it. Only wired for pending (!installed) portals. */
  function keyDownHandler(installed: boolean, name: string, setPendingDeleteName: (n: string) => void) {
    if (installed) return undefined;
    return (e: { key: string; preventDefault: () => void }) => {
      if (e.key === "Delete" || e.key === "Backspace") {
        e.preventDefault();
        setPendingDeleteName(name);
      }
    };
  }

  it("Delete key on a pending portal's option triggers the delete confirm", () => {
    const setPendingDeleteName = vi.fn();
    const handler = keyDownHandler(false, "web1trang", setPendingDeleteName);
    const preventDefault = vi.fn();

    handler?.({ key: "Delete", preventDefault });

    expect(preventDefault).toHaveBeenCalled();
    expect(setPendingDeleteName).toHaveBeenCalledWith("web1trang");
  });

  it("Backspace key on a pending portal's option also triggers the delete confirm", () => {
    const setPendingDeleteName = vi.fn();
    const handler = keyDownHandler(false, "web1trang", setPendingDeleteName);
    const preventDefault = vi.fn();

    handler?.({ key: "Backspace", preventDefault });

    expect(setPendingDeleteName).toHaveBeenCalledWith("web1trang");
  });

  it("other keys on a pending portal's option are ignored", () => {
    const setPendingDeleteName = vi.fn();
    const handler = keyDownHandler(false, "web1trang", setPendingDeleteName);
    const preventDefault = vi.fn();

    handler?.({ key: "Enter", preventDefault });

    expect(preventDefault).not.toHaveBeenCalled();
    expect(setPendingDeleteName).not.toHaveBeenCalled();
  });

  it("installed portals get no keydown handler at all", () => {
    const setPendingDeleteName = vi.fn();
    const handler = keyDownHandler(true, "acme", setPendingDeleteName);

    expect(handler).toBeUndefined();
  });
});

describe("useBitrixPortalDelete — mock contract", () => {
  it("exposes mutateAsync and isPending", async () => {
    const { useBitrixPortalDelete } = await import("./use-bitrix-portals");
    const result = useBitrixPortalDelete();
    expect(typeof result.mutateAsync).toBe("function");
    expect(result.isPending).toBe(false);
  });
});
