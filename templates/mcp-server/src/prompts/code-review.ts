import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

const schema = {
  code: z.string().describe("The code to review"),
  language: z.string().default("typescript").describe("Programming language"),
};

export function registerCodeReviewPrompt(server: McpServer) {
  server.prompt(
    "code_review",
    "Generate a code review prompt for the given code",
    schema,
    ({ code, language }) => ({
      messages: [
        {
          role: "user" as const,
          content: {
            type: "text" as const,
            text: [
              `Please review the following ${language} code:`,
              "",
              "```" + language,
              code,
              "```",
              "",
              "Focus on:",
              "1. Correctness and potential bugs",
              "2. Performance issues",
              "3. Security vulnerabilities",
              "4. Code style and readability",
              "5. Suggested improvements",
            ].join("\n"),
          },
        },
      ],
    }),
  );
}
