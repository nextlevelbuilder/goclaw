import { test, expect, describe } from "bun:test";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { createServer } from "../src/index.ts";

async function createTestClient() {
  const server = createServer();
  const client = new Client({ name: "test-client", version: "1.0.0" });
  const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();

  await Promise.all([client.connect(clientTransport), server.connect(serverTransport)]);

  return client;
}

describe("tools", () => {
  test("greet returns greeting message", async () => {
    const client = await createTestClient();
    const result = await client.callTool({ name: "greet", arguments: { name: "World" } });

    expect(result.content).toEqual([
      { type: "text", text: "Hello, World! Welcome to the MCP server." },
    ]);
  });

  test("fetch_url blocks internal addresses", async () => {
    const client = await createTestClient();
    const result = await client.callTool({
      name: "fetch_url",
      arguments: { url: "http://127.0.0.1/secret" },
    });

    expect(result.isError).toBe(true);
    expect((result.content as Array<{ text: string }>)[0]?.text).toContain("Blocked");
  });

  test("lists available tools", async () => {
    const client = await createTestClient();
    const { tools } = await client.listTools();

    const names = tools.map((t) => t.name);
    expect(names).toContain("greet");
    expect(names).toContain("fetch_url");
  });
});

describe("resources", () => {
  test("lists available resources", async () => {
    const client = await createTestClient();
    const { resources } = await client.listResources();

    expect(resources.length).toBeGreaterThan(0);
    expect(resources[0]?.uri).toBe("info://server");
  });

  test("reads server-info resource", async () => {
    const client = await createTestClient();
    const result = await client.readResource({ uri: "info://server" });

    const content = result.contents[0];
    expect(content?.mimeType).toBe("application/json");
    const data = JSON.parse(content?.text as string);
    expect(data.runtime).toBe("bun");
  });
});

describe("prompts", () => {
  test("lists available prompts", async () => {
    const client = await createTestClient();
    const { prompts } = await client.listPrompts();

    const names = prompts.map((p) => p.name);
    expect(names).toContain("code_review");
  });

  test("gets code_review prompt", async () => {
    const client = await createTestClient();
    const result = await client.getPrompt({
      name: "code_review",
      arguments: { code: "const x = 1;", language: "typescript" },
    });

    expect(result.messages.length).toBe(1);
    expect(result.messages[0]?.role).toBe("user");
  });
});
