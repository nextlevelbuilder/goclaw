import { describe, expect, it } from "vitest";
import { resolveBootstrapState } from "../resolve-bootstrap-state";

const admin = {
  canManageProviders: true,
  providersLoadFailed: false,
  skipped: false,
  hasProvider: true,
  hasAgent: true,
};
const complete = { needsSetup: false, currentStep: "complete" };

describe("resolveBootstrapState", () => {
  it("never sends a role that cannot manage providers to setup", () => {
    expect(
      resolveBootstrapState({ ...admin, canManageProviders: false, hasProvider: false, hasAgent: false }),
    ).toEqual(complete);
  });

  it("does not read a failed provider load as having no providers", () => {
    expect(resolveBootstrapState({ ...admin, providersLoadFailed: true, hasProvider: false })).toEqual(complete);
  });

  it("starts admins without a provider at step 1", () => {
    expect(resolveBootstrapState({ ...admin, hasProvider: false, hasAgent: false })).toEqual({
      needsSetup: true,
      currentStep: 1,
    });
  });

  it("starts admins without an agent at step 2", () => {
    expect(resolveBootstrapState({ ...admin, hasAgent: false })).toEqual({ needsSetup: true, currentStep: 2 });
  });

  it("honours a skipped setup", () => {
    expect(resolveBootstrapState({ ...admin, skipped: true, hasProvider: false })).toEqual(complete);
  });

  it("is complete once a provider and an agent exist", () => {
    expect(resolveBootstrapState(admin)).toEqual(complete);
  });
});
