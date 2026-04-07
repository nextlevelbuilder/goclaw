/**
 * remark-wikilinks — transforms [[target]] syntax into wikilink AST nodes
 * for rendering as styled pill links in MarkdownRenderer.
 */
import { visit } from "unist-util-visit";
import type { Root, Text, PhrasingContent } from "mdast";
import type { Plugin } from "unified";

const WIKILINK_RE = /\[\[([^\]]+)\]\]/g;

/**
 * Splits a text node into an array of text/wikilink nodes.
 * Returns null if no wikilinks found.
 */
function splitTextNode(node: Text): PhrasingContent[] | null {
  const { value } = node;
  if (!value.includes("[[")) return null;

  const result: PhrasingContent[] = [];
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  WIKILINK_RE.lastIndex = 0;
  while ((match = WIKILINK_RE.exec(value)) !== null) {
    const [fullMatch, target] = match;
    const start = match.index;

    if (start > lastIndex) {
      result.push({ type: "text", value: value.slice(lastIndex, start) });
    }

    // Custom node type — rehype will pass through via hast element
    result.push({
      type: "wikilink" as "text",
      value: fullMatch,
      data: {
        hName: "wikilink",
        hProperties: { target: target?.trim() ?? "" },
      },
    } as unknown as PhrasingContent);

    lastIndex = start + fullMatch.length;
  }

  if (lastIndex < value.length) {
    result.push({ type: "text", value: value.slice(lastIndex) });
  }

  return result.length > 0 ? result : null;
}

/**
 * Remark plugin that converts [[wikilink]] syntax into custom hast nodes.
 */
const remarkWikilinks: Plugin<[], Root> = () => (tree) => {
  visit(tree, "text", (node: Text, index, parent) => {
    if (!parent || index == null) return;

    const parts = splitTextNode(node);
    if (!parts) return;

    // Replace the single text node with the split array
    parent.children.splice(index, 1, ...(parts as typeof parent.children));

    // Skip over newly inserted nodes (return index to prevent infinite loop)
    return index + parts.length;
  });
};

export default remarkWikilinks;
