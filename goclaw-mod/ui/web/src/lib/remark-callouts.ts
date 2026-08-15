/**
 * remark-callouts — transforms Obsidian-style callout blockquotes into
 * custom hast elements for styled rendering.
 *
 * Syntax: > [!type] Optional Title
 *         > Content lines...
 *
 * Supported types: note, warning, tip, danger, info, important
 */
import { visit } from "unist-util-visit";
import type { Root, Blockquote, Paragraph, Text } from "mdast";
import type { Plugin } from "unified";

export type CalloutType = "note" | "warning" | "tip" | "danger" | "info" | "important";

const CALLOUT_TYPES = new Set<string>(["note", "warning", "tip", "danger", "info", "important"]);

// Matches: [!type] or [!type] Title text
const CALLOUT_RE = /^\[!([\w-]+)\][ \t]*(.*)/i;

function parseCalloutHeader(
  blockquote: Blockquote
): { type: CalloutType; title: string } | null {
  const firstChild = blockquote.children[0];
  if (firstChild?.type !== "paragraph") return null;

  const firstText = firstChild.children[0];
  if (firstText?.type !== "text") return null;

  // The first line of the first paragraph
  const firstLine = firstText.value.split("\n")[0] ?? "";
  const match = CALLOUT_RE.exec(firstLine.trim());
  if (!match) return null;

  const rawType = match[1]?.toLowerCase() ?? "";
  if (!CALLOUT_TYPES.has(rawType)) return null;

  return {
    type: rawType as CalloutType,
    title: match[2]?.trim() ?? "",
  };
}

/**
 * Strip the [!type] Title line from the first paragraph, leaving remaining
 * content intact. Returns cleaned blockquote children.
 */
function stripCalloutHeader(blockquote: Blockquote): Blockquote["children"] {
  const [firstPara, ...rest] = blockquote.children;
  if (firstPara?.type !== "paragraph") return blockquote.children;

  const para = firstPara as Paragraph;
  const [firstText, ...restTexts] = para.children;

  if (firstText?.type !== "text") return blockquote.children;

  // Remove the first line from the text node
  const lines = firstText.value.split("\n");
  const remaining = lines.slice(1).join("\n").trimStart();

  if (!remaining && restTexts.length === 0) {
    // First paragraph was only the header — drop it entirely
    return rest;
  }

  const newFirstText: Text = { ...firstText, value: remaining };
  const newPara: Paragraph = { ...para, children: [newFirstText, ...restTexts] };
  return [newPara, ...rest];
}

/**
 * Remark plugin that converts [!type] blockquotes into custom callout elements.
 */
const remarkCallouts: Plugin<[], Root> = () => (tree) => {
  visit(tree, "blockquote", (node: Blockquote, index, parent) => {
    if (!parent || index == null) return;

    const parsed = parseCalloutHeader(node);
    if (!parsed) return;

    const { type, title } = parsed;
    const cleanedChildren = stripCalloutHeader(node);

    // Replace blockquote with a custom callout node
    const calloutNode = {
      type: "callout" as "blockquote",
      data: {
        hName: "callout",
        hProperties: {
          calloutType: type,
          calloutTitle: title || type.charAt(0).toUpperCase() + type.slice(1),
        },
      },
      children: cleanedChildren,
    } as unknown as Blockquote;

    parent.children.splice(index, 1, calloutNode);
  });
};

export default remarkCallouts;
