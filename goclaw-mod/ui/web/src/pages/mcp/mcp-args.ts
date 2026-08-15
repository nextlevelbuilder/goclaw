/** Split argv text using shell-like whitespace rules. Commas are literal. */
export function parseShellArgs(input: string): string[] {
  const tokens: string[] = [];
  let token = "";
  let tokenStarted = false;
  let quote: "'" | "\"" | null = null;
  let escaping = false;

  const pushToken = () => {
    if (tokenStarted) {
      tokens.push(token);
      token = "";
      tokenStarted = false;
    }
  };

  for (const char of input.trim()) {
    if (escaping) {
      token += char;
      tokenStarted = true;
      escaping = false;
      continue;
    }

    if (char === "\\" && quote !== "'") {
      tokenStarted = true;
      escaping = true;
      continue;
    }

    if ((char === "'" || char === "\"") && !quote) {
      tokenStarted = true;
      quote = char;
      continue;
    }

    if (quote === char) {
      quote = null;
      continue;
    }

    if (!quote && /\s/.test(char)) {
      pushToken();
      continue;
    }

    token += char;
    tokenStarted = true;
  }

  if (escaping) token += "\\";
  pushToken();
  return tokens;
}

/** Render argv text without inventing comma separators between arguments. */
export function formatShellArgs(args: string[]): string {
  return args.map(formatShellArg).join(" ");
}

function formatShellArg(arg: string): string {
  if (arg === "") return "\"\"";
  if (!/[\s"\\]/.test(arg)) return arg;
  return `"${arg.replace(/(["\\])/g, "\\$1")}"`;
}
