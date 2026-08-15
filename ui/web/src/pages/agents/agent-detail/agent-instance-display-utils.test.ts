import { describe, expect, it } from "vitest";
import type { ChannelContact } from "@/types/contact";
import { buildAgentInstanceDisplay, getAgentInstanceResolveIds } from "./agent-instance-display-utils";

function resolver(contacts: Record<string, Partial<ChannelContact>>) {
  return (id: string): ChannelContact | null => (contacts[id] as ChannelContact | undefined) ?? null;
}

describe("agent instance display utils", () => {
  it("formats group instances as channel, chat name, and raw instance id", () => {
    const display = buildAgentInstanceDisplay(
      {
        user_id: "group:lark-cppai-pm:oc_123",
        metadata: { chat_title: "LVC Online" },
      },
      resolver({}),
    );

    expect(display.label).toBe("Lark - LVC Online - oc_123");
    expect(display.instanceId).toBe("oc_123");
  });

  it("uses raw group contact names when profile metadata has no title", () => {
    const display = buildAgentInstanceDisplay(
      { user_id: "group:lark-cppai-pm:oc_456" },
      resolver({
        oc_456: {
          sender_id: "oc_456",
          channel_type: "feishu",
          display_name: "Peacefullfie",
        },
      }),
    );

    expect(display.label).toBe("Lark - Peacefullfie - oc_456");
  });

  it("does not use sender display names as group names", () => {
    const display = buildAgentInstanceDisplay(
      {
        user_id: "group:lark-cppai-pm:oc_456",
        metadata: { display_name: "Last Sender" },
      },
      resolver({}),
    );

    expect(display.label).toBe("Lark - oc_456");
  });

  it("formats direct channel contacts when contact metadata is available", () => {
    const display = buildAgentInstanceDisplay(
      { user_id: "ou_789" },
      resolver({
        ou_789: {
          sender_id: "ou_789",
          channel_type: "feishu",
          channel_instance: "lark-cppai-pm",
          display_name: "Duc Nguyen",
        },
      }),
    );

    expect(display.label).toBe("Lark - Duc Nguyen - ou_789");
  });

  it("falls back to a clean raw id when no channel or name is known", () => {
    const display = buildAgentInstanceDisplay({ user_id: "system" }, resolver({}));

    expect(display.label).toBe("system");
  });

  it("detects underscore channel prefixes", () => {
    const display = buildAgentInstanceDisplay({ user_id: "group:zalo_ol:chat4878" }, resolver({}));

    expect(display.label).toBe("Zalo - chat4878");
  });

  it("uses raw group ids for contact resolver inputs", () => {
    expect(
      getAgentInstanceResolveIds([
        { user_id: "group:telegram:-100123" },
        { user_id: "group:telegram:-100123" },
        { user_id: "ou_abc" },
      ]),
    ).toEqual(["-100123", "ou_abc"]);
  });

  it("maps tele group instance prefixes to Telegram", () => {
    const display = buildAgentInstanceDisplay({ user_id: "group:tele-itsdd-local-goclaw:-5104" }, resolver({}));

    expect(display.label).toBe("Telegram - -5104");
  });
});
