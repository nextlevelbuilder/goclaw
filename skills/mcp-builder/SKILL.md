---
name: mcp-builder
slug: mcp-builder
description: Guide for building and registering MCP (Model Context Protocol) servers that enable agents to interact with external services. Use when creating MCP servers in Python (FastMCP) or TypeScript (MCP SDK), testing MCP connections, evaluating MCP server quality, or registering MCP servers into GoClaw for agent use.
license: Complete terms in LICENSE.txt
metadata:
  author: GoClaw
  version: "1.0.0"
---

# MCP Server Development Guide

## Overview

Create MCP (Model Context Protocol) servers that enable LLMs to interact with external services through well-designed tools. The quality of an MCP server is measured by how well it enables LLMs to accomplish real-world tasks.

---

# Process

## High-Level Workflow

Creating a high-quality MCP server involves five main phases:

### Phase 1: Deep Research and Planning

#### 1.1 Understand Modern MCP Design

**API Coverage vs. Workflow Tools:**
Balance comprehensive API endpoint coverage with specialized workflow tools. Workflow tools can be more convenient for specific tasks, while comprehensive coverage gives agents flexibility to compose operations. Performance varies by client—some clients benefit from code execution that combines basic tools, while others work better with higher-level workflows. When uncertain, prioritize comprehensive API coverage.

**Tool Naming and Discoverability:**
Clear, descriptive tool names help agents find the right tools quickly. Use consistent prefixes (e.g., `github_create_issue`, `github_list_repos`) and action-oriented naming.

**Context Management:**
Agents benefit from concise tool descriptions and the ability to filter/paginate results. Design tools that return focused, relevant data. Some clients support code execution which can help agents filter and process data efficiently.

**Actionable Error Messages:**
Error messages should guide agents toward solutions with specific suggestions and next steps.

#### 1.2 Study MCP Protocol Documentation

**Navigate the MCP specification:**

Start with the sitemap to find relevant pages: `https://modelcontextprotocol.io/sitemap.xml`

Then fetch specific pages with `.md` suffix for markdown format (e.g., `https://modelcontextprotocol.io/specification/draft.md`).

Key pages to review:
- Specification overview and architecture
- Transport mechanisms (streamable HTTP, stdio)
- Tool, resource, and prompt definitions

#### 1.3 Study Framework Documentation

**Recommended stack:**
- **Language**: TypeScript (high-quality SDK support and good compatibility in many execution environments e.g. MCPB. Plus AI models are good at generating TypeScript code, benefiting from its broad usage, static typing and good linting tools)
- **Transport**: Streamable HTTP for remote servers, using stateless JSON (simpler to scale and maintain, as opposed to stateful sessions and streaming responses). stdio for local servers.

**Load framework documentation:**

- **MCP Best Practices**: [View Best Practices](./references/mcp_best_practices.md) - Core guidelines

**For TypeScript (recommended):**
- **TypeScript SDK**: Use WebFetch to load `https://raw.githubusercontent.com/modelcontextprotocol/typescript-sdk/main/README.md`
- [TypeScript Guide](./references/node_mcp_server.md) - TypeScript patterns and examples

**For Python:**
- **Python SDK**: Use WebFetch to load `https://raw.githubusercontent.com/modelcontextprotocol/python-sdk/main/README.md`
- [Python Guide](./references/python_mcp_server.md) - Python patterns and examples

#### 1.4 Plan Your Implementation

**Understand the API:**
Review the service's API documentation to identify key endpoints, authentication requirements, and data models. Use web search and WebFetch as needed.

**Tool Selection:**
Prioritize comprehensive API coverage. List endpoints to implement, starting with the most common operations.

---

### Phase 2: Implementation

#### 2.1 Set Up Project Structure

See language-specific guides for project setup:
- [TypeScript Guide](./references/node_mcp_server.md) - Project structure, package.json, tsconfig.json
- [Python Guide](./references/python_mcp_server.md) - Module organization, dependencies

#### 2.2 Implement Core Infrastructure

Create shared utilities:
- API client with authentication
- Error handling helpers
- Response formatting (JSON/Markdown)
- Pagination support

#### 2.3 Implement Tools

For each tool:

**Input Schema:**
- Use Zod (TypeScript) or Pydantic (Python)
- Include constraints and clear descriptions
- Add examples in field descriptions

**Output Schema:**
- Define `outputSchema` where possible for structured data
- Use `structuredContent` in tool responses (TypeScript SDK feature)
- Helps clients understand and process tool outputs

**Tool Description:**
- Concise summary of functionality
- Parameter descriptions
- Return type schema

**Implementation:**
- Async/await for I/O operations
- Proper error handling with actionable messages
- Support pagination where applicable
- Return both text content and structured data when using modern SDKs

**Annotations:**
- `readOnlyHint`: true/false
- `destructiveHint`: true/false
- `idempotentHint`: true/false
- `openWorldHint`: true/false

---

### Phase 3: Review and Test

#### 3.1 Code Quality

Review for:
- No duplicated code (DRY principle)
- Consistent error handling
- Full type coverage
- Clear tool descriptions

#### 3.2 Build and Test

**TypeScript:**
- Run `npm run build` to verify compilation
- Test with MCP Inspector: `npx @modelcontextprotocol/inspector`

**Python:**
- Verify syntax: `python -m py_compile your_server.py`
- Test with MCP Inspector

See language-specific guides for detailed testing approaches and quality checklists.

---

### Phase 4: Create Evaluations

After implementing your MCP server, create comprehensive evaluations to test its effectiveness.

**Load [Evaluation Guide](./references/evaluation.md) for complete evaluation guidelines.**

#### 4.1 Understand Evaluation Purpose

Use evaluations to test whether LLMs can effectively use your MCP server to answer realistic, complex questions.

#### 4.2 Create 10 Evaluation Questions

To create effective evaluations, follow the process outlined in the evaluation guide:

1. **Tool Inspection**: List available tools and understand their capabilities
2. **Content Exploration**: Use READ-ONLY operations to explore available data
3. **Question Generation**: Create 10 complex, realistic questions
4. **Answer Verification**: Solve each question yourself to verify answers

#### 4.3 Evaluation Requirements

Ensure each question is:
- **Independent**: Not dependent on other questions
- **Read-only**: Only non-destructive operations required
- **Complex**: Requiring multiple tool calls and deep exploration
- **Realistic**: Based on real use cases humans would care about
- **Verifiable**: Single, clear answer that can be verified by string comparison
- **Stable**: Answer won't change over time

#### 4.4 Output Format

Create an XML file with this structure:

```xml
<evaluation>
  <qa_pair>
    <question>Your question here</question>
    <answer>Expected answer</answer>
  </qa_pair>
<!-- More qa_pairs... -->
</evaluation>
```

---

### Phase 5: Deploy and Register in GoClaw

After building and testing your MCP server, deploy it and register in GoClaw so agents can use its tools.

#### 5.1 Choose Deployment Target

| Target | Transport | When to Use |
|--------|-----------|-------------|
| Local process | `stdio` | Development, single-machine setups |
| Remote server | `streamable-http` | Dedicated VM or bare-metal |
| **Kubernetes** | `streamable-http` | Production, scaling, HA — see [K8s Deployment Guide](./references/kubernetes-mcp-deployment.md) |

#### 5.2 Local Registration

```
register_mcp_server({
  "name": "my-api-server",
  "transport": "stdio",
  "command": "node",
  "args": ["dist/index.js"],
  "display_name": "My API Server"
})
```

#### 5.3 Kubernetes Deployment and Registration

**CRITICAL: Verify kubeconfig before any kubectl/helm operation.**

```bash
kubectl config current-context
kubectl cluster-info
```

Deploy via Helm chart, then **get the real K8s node IP and NodePort** for registration:

```bash
# Deploy
helm install my-mcp ./mcp-server-chart \
  --namespace mcp-servers \
  --set image.repository=<registry>/<name>-mcp-server

# IMPORTANT: Get the REAL node IP and NodePort — NEVER use localhost!
# GoClaw runs inside Docker, so localhost won't reach the K8s cluster.
export NODE_PORT=$(kubectl get svc my-mcp-mcp-server -n mcp-servers -o jsonpath='{.spec.ports[0].nodePort}')
export NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
echo "MCP server URL: http://$NODE_IP:$NODE_PORT"
```

**WARNING: Do NOT use `localhost` or `127.0.0.1` as the URL.** GoClaw runs in a Docker container — `localhost` refers to the container itself, not the K8s cluster. You MUST use the actual K8s node IP (InternalIP) obtained from `kubectl get nodes`.

Then register in GoClaw using the **real node IP**:

```
register_mcp_server({
  "name": "my-mcp-k8s",
  "transport": "streamable-http",
  "url": "http://<NODE_IP>:<NODE_PORT>",
  "display_name": "My MCP Server (K8s)",
  "headers": {"Authorization": "Bearer <token>"},
  "timeout_sec": 30
})
```

**Verify the URL is reachable from inside the GoClaw container** before registering:
```bash
# Test connectivity from GoClaw container
exec({ "command": "wget -qO- http://<NODE_IP>:<NODE_PORT>/health || echo 'UNREACHABLE'" })
```

Full guide with Helm chart templates, Dockerfile, HPA, probes, and best practices: [Kubernetes MCP Deployment](./references/kubernetes-mcp-deployment.md)

#### 5.4 What Happens on Register

1. Server config is validated and saved to `mcp_servers` table
2. Sensitive fields (API keys, headers, env vars) are AES-256-GCM encrypted
3. Server is auto-granted to the calling agent
4. Server tools become available to the agent on next turn

#### 5.5 Grant to Other Agents

After registering, grant the MCP server to other agents via the web UI (MCP Servers page) or HTTP API:
- `POST /v1/mcp/servers/{id}/grants/agent` with `{"agent_id": "...", "enabled": true}`

See `references/goclaw-mcp-integration.md` for the full native integration guide.

---

# Reference Files

Load these resources as needed during development:

| Phase | Resource | Description |
|-------|----------|-------------|
| 1 | **MCP Protocol** — `https://modelcontextprotocol.io/sitemap.xml` | Spec overview, transports, tool definitions |
| 1 | [MCP Best Practices](./references/mcp_best_practices.md) | Naming, response formats, pagination, security |
| 1-2 | **Python SDK** — fetch `https://raw.githubusercontent.com/modelcontextprotocol/python-sdk/main/README.md` | Official Python SDK docs |
| 1-2 | **TypeScript SDK** — fetch `https://raw.githubusercontent.com/modelcontextprotocol/typescript-sdk/main/README.md` | Official TypeScript SDK docs |
| 2 | [Python Guide](./references/python_mcp_server.md) | FastMCP patterns, Pydantic, working examples |
| 2 | [TypeScript Guide](./references/node_mcp_server.md) | Zod schemas, project structure, working examples |
| 4 | [Evaluation Guide](./references/evaluation.md) | QA creation, XML format, running evals |
| 5 | [GoClaw Integration](./references/goclaw-mcp-integration.md) | `register_mcp_server` tool, transport config, grants |
| 5 | [Kubernetes Deployment](./references/kubernetes-mcp-deployment.md) | Helm chart, kubectl, NodePort/IP, Dockerfile, HPA |
