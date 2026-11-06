/**
 * Tests for pasting images into the chat input and the DropZone disabled gate.
 * Rendered with react-dom/client + act (jsdom), per the existing test convention.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { ChatInput, type AttachedFile } from "@/components/chat/chat-input";
import { DropZone } from "@/components/chat/drop-zone";
import { extractPastedImages } from "@/components/chat/paste-images";

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (k: string) => k }) }));
vi.mock("@/hooks/use-voice-recorder", () => ({
  useVoiceRecorder: () => ({
    isRecording: false,
    duration: 0,
    startRecording: vi.fn(),
    stopRecording: vi.fn(),
    cancelRecording: vi.fn(),
  }),
}));

function clipboard(items: { kind: string; type: string; file?: File | null }[]) {
  return {
    items: items.map((i) => ({ kind: i.kind, type: i.type, getAsFile: () => i.file ?? null })),
  } as unknown as DataTransfer;
}

function pasteEvent(data: DataTransfer) {
  const ev = new Event("paste", { bubbles: true, cancelable: true });
  Object.defineProperty(ev, "clipboardData", { value: data });
  return ev;
}

describe("extractPastedImages", () => {
  it("returns nothing for text-only paste", () => {
    expect(extractPastedImages(clipboard([{ kind: "string", type: "text/plain" }]))).toEqual([]);
    expect(extractPastedImages(null)).toEqual([]);
  });

  it("names unnamed images with a timestamp and a mapped extension", () => {
    const blob = new File(["x"], "", { type: "image/jpeg" });
    const [f] = extractPastedImages(clipboard([{ kind: "file", type: "image/jpeg", file: blob }]), 1234);
    expect(f?.name).toBe("pasted-1234.jpg");
    expect(f?.type).toBe("image/jpeg");
  });

  it("keeps real file names and ignores non-image files", () => {
    const named = new File(["x"], "shot.png", { type: "image/png" });
    const pdf = new File(["x"], "a.pdf", { type: "application/pdf" });
    const out = extractPastedImages(
      clipboard([
        { kind: "file", type: "image/png", file: named },
        { kind: "file", type: "application/pdf", file: pdf },
      ]),
    );
    expect(out.map((f) => f.name)).toEqual(["shot.png"]);
  });

  it("gives distinct names to several unnamed images", () => {
    const a = new File(["a"], "", { type: "image/png" });
    const b = new File(["b"], "", { type: "image/png" });
    const out = extractPastedImages(
      clipboard([
        { kind: "file", type: "image/png", file: a },
        { kind: "file", type: "image/png", file: b },
      ]),
      99,
    );
    expect(out.map((f) => f.name)).toEqual(["pasted-99.png", "pasted-99-1.png"]);
  });
});

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
});
afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

function renderInput(props: { isBusy?: boolean; disabled?: boolean }) {
  const onFilesChange = vi.fn<(f: AttachedFile[]) => void>();
  act(() => {
    root.render(
      <ChatInput
        onSend={vi.fn()}
        onAbort={vi.fn()}
        isBusy={props.isBusy ?? false}
        disabled={props.disabled}
        files={[]}
        onFilesChange={onFilesChange}
      />,
    );
  });
  return { onFilesChange, textarea: container.querySelector("textarea")! };
}

describe("ChatInput paste", () => {
  it("attaches a pasted image and prevents the default paste", () => {
    const { onFilesChange, textarea } = renderInput({});
    const img = new File(["x"], "", { type: "image/png" });
    const ev = pasteEvent(clipboard([{ kind: "file", type: "image/png", file: img }]));
    act(() => {
      textarea.dispatchEvent(ev);
    });
    expect(ev.defaultPrevented).toBe(true);
    expect(onFilesChange).toHaveBeenCalledTimes(1);
    expect(onFilesChange.mock.calls[0]?.[0][0]?.file.name).toMatch(/^pasted-\d+\.png$/);
  });

  it("leaves plain text paste alone", () => {
    const { onFilesChange, textarea } = renderInput({});
    const ev = pasteEvent(clipboard([{ kind: "string", type: "text/plain" }]));
    act(() => {
      textarea.dispatchEvent(ev);
    });
    expect(ev.defaultPrevented).toBe(false);
    expect(onFilesChange).not.toHaveBeenCalled();
  });

  it("does not attach images while the agent is busy", () => {
    const { onFilesChange, textarea } = renderInput({ isBusy: true });
    const img = new File(["x"], "a.png", { type: "image/png" });
    act(() => {
      textarea.dispatchEvent(pasteEvent(clipboard([{ kind: "file", type: "image/png", file: img }])));
    });
    expect(onFilesChange).not.toHaveBeenCalled();
  });
});

function dropEvent(files: File[]) {
  const ev = new Event("drop", { bubbles: true, cancelable: true });
  Object.defineProperty(ev, "dataTransfer", { value: { files } });
  return ev;
}

describe("DropZone", () => {
  it("accepts dropped files when enabled", () => {
    const onDrop = vi.fn();
    act(() => root.render(<DropZone onDrop={onDrop}><span>x</span></DropZone>));
    const f = new File(["x"], "a.txt");
    const ev = dropEvent([f]);
    act(() => {
      container.firstElementChild!.dispatchEvent(ev);
    });
    expect(onDrop).toHaveBeenCalledWith([f]);
    expect(ev.defaultPrevented).toBe(true);
  });

  it("swallows but ignores drops when disabled", () => {
    const onDrop = vi.fn();
    act(() => root.render(<DropZone onDrop={onDrop} disabled><span>x</span></DropZone>));
    const ev = dropEvent([new File(["x"], "a.txt")]);
    act(() => {
      container.firstElementChild!.dispatchEvent(ev);
    });
    expect(onDrop).not.toHaveBeenCalled();
    expect(ev.defaultPrevented).toBe(true); // browser must not open the file
  });
});
