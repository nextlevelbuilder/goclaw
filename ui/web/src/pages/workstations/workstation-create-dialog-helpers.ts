import type { CreateWorkstationParams, WorkstationBackendType } from "./hooks/use-workstations";

/** Auth method for the SSH backend. The gateway accepts inline PEM or a password. */
export type SshAuthMethod = "privateKey" | "password";

export interface WorkstationCreateFormState {
  key: string;
  name: string;
  backend: WorkstationBackendType;
  // SSH
  host: string;
  port: string;
  user: string;
  authMethod: SshAuthMethod;
  privateKey: string;
  password: string;
  // Docker
  container: string;
  image: string;
  socketPath: string;
}

export type BuildCreatePayloadResult =
  | { kind: "ok"; payload: CreateWorkstationParams }
  | { kind: "error"; errorKey: string };

/**
 * Builds the workstations.create RPC payload from dialog form state.
 *
 * Field names here must match the gateway wire contract exactly — the WS client
 * serializes params verbatim with no case conversion, and Go's json decoder
 * silently drops unknown members rather than erroring. A mismatched name
 * therefore surfaces as "<field> is required" rather than as a decode failure.
 *
 * Metadata shape mirrors store.SSHMetadata / store.DockerMetadata: SSH requires
 * privateKey or password, and Docker requires an image plus host or socketPath.
 */
export function buildWorkstationCreatePayload(
  form: WorkstationCreateFormState,
): BuildCreatePayloadResult {
  const key = form.key.trim();
  const name = form.name.trim();

  if (!key) return { kind: "error", errorKey: "keyRequired" };
  if (!name) return { kind: "error", errorKey: "nameRequired" };

  let metadata: Record<string, unknown>;

  if (form.backend === "ssh") {
    const host = form.host.trim();
    const user = form.user.trim();
    if (!host || !user) return { kind: "error", errorKey: "sshHostUserRequired" };

    const port = parseInt(form.port, 10) || 22;
    if (port < 1 || port > 65535) return { kind: "error", errorKey: "sshPortRange" };

    const privateKey = form.privateKey.trim();
    const password = form.password;
    if (form.authMethod === "privateKey" && !privateKey) {
      return { kind: "error", errorKey: "sshPrivateKeyRequired" };
    }
    if (form.authMethod === "password" && !password) {
      return { kind: "error", errorKey: "sshPasswordRequired" };
    }

    metadata = {
      host,
      port,
      user,
      ...(form.authMethod === "privateKey" ? { privateKey } : { password }),
    };
  } else {
    // The gateway stores the container name in DockerMetadata.host and surfaces
    // it back as metadataSummary.containerName.
    const container = form.container.trim();
    const image = form.image.trim();
    const socketPath = form.socketPath.trim();
    if (!container) return { kind: "error", errorKey: "dockerContainerRequired" };
    if (!image) return { kind: "error", errorKey: "dockerImageRequired" };

    metadata = {
      host: container,
      image,
      ...(socketPath ? { socketPath } : {}),
    };
  }

  return {
    kind: "ok",
    payload: {
      workstationKey: key,
      name,
      backendType: form.backend,
      metadata,
    },
  };
}
