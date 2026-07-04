import { describe, expect, it } from "vitest";
import {
  SYSTEM_MESSAGE_LOCALES,
  buildSystemMessagesPatch,
  localizedSystemMessageDescription,
  localizedSystemMessageLabel,
  normalizeSystemMessagesDraft,
  systemMessageDefinitionsFromSchema,
} from "./system-messages-section-utils";

describe("system-messages-section-utils", () => {
  const definitions = [
    {
      key: "pairing.group_required",
      template: "Group {{code}}",
      description: "Sent to an unpaired group chat.",
      labels: { en: "Group pairing required", vi: "Yêu cầu ghép nối nhóm" },
      descriptions: { en: "Sent to an unpaired group chat.", vi: "Gửi khi nhóm chưa được ghép nối." },
      variables: ["code"],
    },
    {
      key: "pairing.approved",
      template: "Approved {{app_name}}",
      description: "Sent after approval.",
      variables: ["app_name"],
    },
  ];


  it("reads localized labels and descriptions from config schema definitions", () => {
    const schema = {
      properties: {
        system_messages: {
          definitions: [definitions[0]],
        },
      },
    };

    const parsed = systemMessageDefinitionsFromSchema(schema);
    const first = parsed[0];

    expect(first).toBeDefined();
    expect(localizedSystemMessageLabel(first!, "vi-VN")).toBe("Yêu cầu ghép nối nhóm");
    expect(localizedSystemMessageDescription(first!, "vi")).toBe("Gửi khi nhóm chưa được ghép nối.");
    expect(localizedSystemMessageLabel({ key: "pairing.account_required", template: "" }, "vi")).toBe("pairing.account_required");
  });

  it("normalizes all supported locales for each known message definition", () => {
    expect(SYSTEM_MESSAGE_LOCALES.map((locale) => locale.code)).toEqual(["en", "vi", "zh", "ko"]);

    const draft = normalizeSystemMessagesDraft(
      {
        default_locale: "vi",
        messages: {
          "pairing.group_required": {
            vi: "Nhóm {{code}}",
          },
        },
      },
      definitions,
    );

    expect(draft["pairing.group_required"]).toMatchObject({
      en: "",
      vi: "Nhóm {{code}}",
      zh: "",
      ko: "",
    });
    expect(draft["pairing.approved"]).toMatchObject({
      en: "",
      vi: "",
      zh: "",
      ko: "",
    });
  });

  it("builds a default delivery locale into the config.patch payload", () => {
    const patch = buildSystemMessagesPatch(
      {
        "pairing.group_required": { en: "", vi: "Nhóm {{code}}", zh: "", ko: "" },
      },
      undefined,
      "vi",
    );

    expect(patch).toEqual({
      default_locale: "vi",
      messages: {
        "pairing.group_required": {
          en: "",
          vi: "Nhóm {{code}}",
          zh: "",
          ko: "",
        },
      },
    });
  });

  it("builds a config.patch payload and preserves cleared overrides", () => {
    const patch = buildSystemMessagesPatch({
      "pairing.group_required": { en: "Group {{code}}", vi: "", zh: "", ko: "" },
    });

    expect(patch).toEqual({
      messages: {
        "pairing.group_required": {
          en: "Group {{code}}",
          vi: "",
          zh: "",
          ko: "",
        },
      },
    });
  });

  it("keeps an existing key with empty locales so config.patch can clear old overrides", () => {
    const patch = buildSystemMessagesPatch(
      {
        "pairing.group_required": { en: "", vi: "", zh: "", ko: "" },
      },
      {
        messages: {
          "pairing.group_required": { en: "Old" },
        },
      },
    );

    expect(patch).toEqual({
      messages: {
        "pairing.group_required": {
          en: "",
          vi: "",
          zh: "",
          ko: "",
        },
      },
    });
  });
});
