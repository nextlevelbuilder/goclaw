import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useProviders } from "../use-providers";

const get = vi.hoisted(() => vi.fn());

vi.mock("@/hooks/use-ws", () => ({ useHttp: () => ({ get }) }));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const flush = () => act(() => new Promise<void>((resolve) => setTimeout(resolve, 20)));

function Consumer({ onResult }: { onResult?: (r: ReturnType<typeof useProviders>) => void }) {
  const result = useProviders();
  onResult?.(result);
  return null;
}

describe("useProviders after a failed load", () => {
  let root: Root | undefined;

  afterEach(() => {
    act(() => root?.unmount());
    get.mockReset();
  });

  it("reports the failure and does not reload when another consumer mounts", async () => {
    get.mockRejectedValue(new Error("forbidden"));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    root = createRoot(document.createElement("div"));
    let latest: ReturnType<typeof useProviders> | undefined;
    const render = (consumers: number) =>
      act(() =>
        root!.render(
          <QueryClientProvider client={client}>
            <Consumer onResult={(r) => (latest = r)} />
            {consumers > 1 && <Consumer />}
          </QueryClientProvider>,
        ),
      );

    render(1);
    await flush();
    expect(latest?.loadFailed).toBe(true);
    expect(latest?.loading).toBe(false);

    render(2);
    await flush();
    expect(get).toHaveBeenCalledTimes(1);
    expect(latest?.loading).toBe(false);
  });
});
