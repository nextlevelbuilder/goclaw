import { describe, expect, it } from "vitest";
import {
  buildWorkstationCreatePayload,
  type WorkstationCreateFormState,
} from "../workstation-create-dialog-helpers";
import type { Workstation } from "../hooks/use-workstations";

function form(overrides: Partial<WorkstationCreateFormState> = {}): WorkstationCreateFormState {
  return {
    key: "dev-server",
    name: "Dev Server",
    backend: "ssh",
    host: "192.168.1.100",
    port: "22",
    user: "ubuntu",
    authMethod: "privateKey",
    privateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----",
    password: "",
    container: "",
    image: "",
    socketPath: "",
    ...overrides,
  };
}

describe("workstation create payload contract", () => {
  it("submits camelCase params the gateway handler actually decodes", () => {
    const result = buildWorkstationCreatePayload(form());

    expect(result.kind).toBe("ok");
    if (result.kind !== "ok") return;

    // Regression guard for #1258: snake_case here decodes to the zero value in
    // Go and the handler rejects the request with "workstationKey is required".
    expect(Object.keys(result.payload).sort()).toEqual([
      "backendType",
      "metadata",
      "name",
      "workstationKey",
    ]);
    expect(result.payload.workstationKey).toBe("dev-server");
    expect(result.payload.backendType).toBe("ssh");
    expect(result.payload).not.toHaveProperty("workstation_key");
    expect(result.payload).not.toHaveProperty("backend_type");
  });

  it("sends SSH credentials as privateKey, not the unsupported identity_file", () => {
    const result = buildWorkstationCreatePayload(form());
    if (result.kind !== "ok") throw new Error("expected ok");

    // store.SSHMetadata accepts privateKey or password only; identity_file was
    // silently dropped and left the request failing metadata validation.
    expect(result.payload.metadata).toEqual({
      host: "192.168.1.100",
      port: 22,
      user: "ubuntu",
      privateKey: expect.stringContaining("BEGIN OPENSSH PRIVATE KEY"),
    });
    expect(result.payload.metadata).not.toHaveProperty("identity_file");
  });

  it("sends a password when password auth is selected", () => {
    const result = buildWorkstationCreatePayload(
      form({ authMethod: "password", privateKey: "", password: "hunter2" }),
    );
    if (result.kind !== "ok") throw new Error("expected ok");

    expect(result.payload.metadata).toMatchObject({ password: "hunter2" });
    expect(result.payload.metadata).not.toHaveProperty("privateKey");
  });

  it("rejects SSH auth with no credential rather than letting the backend 400", () => {
    expect(
      buildWorkstationCreatePayload(form({ privateKey: "" })),
    ).toEqual({ kind: "error", errorKey: "sshPrivateKeyRequired" });

    expect(
      buildWorkstationCreatePayload(form({ authMethod: "password", privateKey: "", password: "" })),
    ).toEqual({ kind: "error", errorKey: "sshPasswordRequired" });
  });

  it("maps the Docker container name onto host and requires an image", () => {
    const ok = buildWorkstationCreatePayload(
      form({ backend: "docker", container: "my-box", image: "ubuntu:24.04", socketPath: "" }),
    );
    if (ok.kind !== "ok") throw new Error("expected ok");

    // store.DockerMetadata carries the container name in `host` and requires
    // `image`; the old payload sent {container, docker_host} and always failed.
    expect(ok.payload.metadata).toEqual({ host: "my-box", image: "ubuntu:24.04" });

    expect(
      buildWorkstationCreatePayload(form({ backend: "docker", container: "my-box", image: "" })),
    ).toEqual({ kind: "error", errorKey: "dockerImageRequired" });
  });

  it("requires a workstation key before hitting the network", () => {
    expect(buildWorkstationCreatePayload(form({ key: "  " }))).toEqual({
      kind: "error",
      errorKey: "keyRequired",
    });
  });
});

describe("workstation list contract", () => {
  // Verbatim body of store.Workstation.SanitizedView() as serialized by the
  // gateway, kept complete even though the UI type is a narrower subset — that
  // is what proves the subset is actually assignable from a real response.
  // If the TS interface drifts back to snake_case this stops compiling.
  const apiResponse = {
    id: "8f2b0f7e-1c4a-4c9e-9f1a-2b3c4d5e6f70",
    workstationKey: "aether",
    tenantId: "11111111-2222-3333-4444-555555555555",
    name: "Aether",
    backendType: "ssh",
    defaultCwd: "/home/ubuntu",
    active: true,
    createdAt: "2026-06-22T15:06:38Z",
    updatedAt: "2026-06-22T15:06:38Z",
    createdBy: "admin",
    metadataSummary: { host: "10.0.0.5", port: 22, user: "ubuntu", hasKey: true },
  } as const;

  it("resolves every field the workstations table renders", () => {
    const ws: Workstation = apiResponse;

    // Regression guard for #1258: these read undefined under the old snake_case
    // interface, rendering a blank key, "backend.undefined" and "Invalid Date".
    expect(ws.workstationKey).toBe("aether");
    expect(ws.backendType).toBe("ssh");
    expect(Number.isNaN(new Date(ws.createdAt).getTime())).toBe(false);
    expect(ws.name).toBe("Aether");
    expect(ws.active).toBe(true);
  });
});
