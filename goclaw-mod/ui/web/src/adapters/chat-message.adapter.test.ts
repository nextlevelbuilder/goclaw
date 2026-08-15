import { describe, expect, it } from "vitest";
import { transformHistoryMessages } from "./chat-message.adapter";
import type { Message } from "@/types/session";

describe("transformHistoryMessages", () => {
  it("moves legacy tagged thinking from assistant content into thinking", () => {
    const messages: Message[] = [
      { role: "assistant", content: "Visible <thinking>hidden reasoning</thinking> answer" },
    ];

    const [msg] = transformHistoryMessages(messages);

    expect(msg?.content).toBe("Visible  answer");
    expect(msg?.thinking).toBe("hidden reasoning");
  });

  it("preserves existing thinking and appends tagged thinking", () => {
    const messages: Message[] = [
      { role: "assistant", content: "Visible <think>tagged</think> answer", thinking: "native" },
    ];

    const [msg] = transformHistoryMessages(messages);

    expect(msg?.content).toBe("Visible  answer");
    expect(msg?.thinking).toBe("native\ntagged");
  });
});
