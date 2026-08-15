export type ThinkTagStreamFilterState = {
  text: string;
  thinking: string;
  thinkingDelta: string;
  pending: string;
  inThinking: boolean;
};

const openThinkTagRe = /<\s*(?:redacted_thinking|think(?:ing)?|thought|antthinking)\b[^>]*>/i;
const closeThinkTagRe = /<\/\s*(?:redacted_thinking|think(?:ing)?|thought|antthinking)\s*>/i;
const thinkOpenPrefixes = ["<redacted_thinking", "<think", "<thinking", "<thought", "<antthinking"];
const thinkClosePrefixes = ["</redacted_thinking", "</think", "</thinking", "</thought", "</antthinking"];

export function createThinkTagStreamFilterState(): ThinkTagStreamFilterState {
  return { text: "", thinking: "", thinkingDelta: "", pending: "", inThinking: false };
}

export function appendFilteredThinkingChunk(
  state: ThinkTagStreamFilterState,
  chunk: string,
): ThinkTagStreamFilterState {
  let input = state.pending + chunk;
  let text = state.text;
  let thinking = state.thinking;
  let thinkingDelta = "";
  let inThinking = state.inThinking;
  let pending = "";

  while (input.length > 0) {
    if (inThinking) {
      const close = closeThinkTagRe.exec(input);
      if (!close) {
        const split = splitTrailingThinkTagPrefix(input, thinkClosePrefixes);
        thinking += split.answer;
        thinkingDelta += split.answer;
        pending = split.pending;
        return { text, thinking, thinkingDelta, pending, inThinking: true };
      }

      const thinkingPart = input.slice(0, close.index);
      thinking += thinkingPart;
      thinkingDelta += thinkingPart;
      input = input.slice(close.index + close[0].length);
      inThinking = false;
      continue;
    }

    const open = openThinkTagRe.exec(input);
    if (!open) {
      const split = splitTrailingThinkTagPrefix(input, thinkOpenPrefixes);
      text += split.answer;
      pending = split.pending;
      break;
    }

    text += input.slice(0, open.index);
    input = input.slice(open.index + open[0].length);
    inThinking = true;
  }

  return { text, thinking, thinkingDelta, pending, inThinking };
}

export function splitThinkingTagsFromText(content: string, existingThinking = ""): { content: string; thinking?: string } {
  const state = appendFilteredThinkingChunk(createThinkTagStreamFilterState(), content);
  const thinking = appendThinking(existingThinking, state.thinking);
  return { content: state.text, thinking: thinking || undefined };
}

function appendThinking(existing: string, addition: string): string {
  const cleanExisting = existing.trim();
  const cleanAddition = addition.trim();
  if (!cleanExisting) return cleanAddition;
  if (!cleanAddition) return cleanExisting;
  return `${cleanExisting}\n${cleanAddition}`;
}

function splitTrailingThinkTagPrefix(input: string, prefixes: string[]): { answer: string; pending: string } {
  const idx = input.lastIndexOf("<");
  if (idx < 0) return { answer: input, pending: "" };

  const suffix = input.slice(idx);
  if (suffix.includes(">") || !isPossibleThinkTagPrefix(suffix, prefixes)) {
    return { answer: input, pending: "" };
  }
  return { answer: input.slice(0, idx), pending: suffix };
}

function isPossibleThinkTagPrefix(suffix: string, prefixes: string[]): boolean {
  const lower = suffix.toLowerCase();
  if (!lower.startsWith("<")) return false;

  const normalized = lower.startsWith("</")
    ? `</${lower.slice(2).trimStart()}`
    : `<${lower.slice(1).trimStart()}`;

  if (normalized === "<" || normalized === "</") return true;
  return prefixes.some((tag) => tag.startsWith(normalized) || normalized.startsWith(tag));
}
