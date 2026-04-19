---
name: openrouter-cli
description: Use this skill when the user wants to interact with OpenRouter API — send chat completions, list/search models, check credits/usage, manage API keys, create embeddings, rerank documents, or generate videos. Activate when user mentions OpenRouter, model browsing, or needs multi-provider LLM access.
---

# OpenRouter CLI Skill

## Overview

OpenRouter CLI (`openrouter`) provides unified access to 300+ LLM models from multiple providers (OpenAI, Anthropic, Google, Meta, Mistral, etc.) via a single API.

## Prerequisites

```bash
# Install
npm install -g @mrgoonie/openrouter-cli

# Authenticate
openrouter auth set-key sk-or-v1-...
# or via env: export OPENROUTER_API_KEY=sk-or-v1-...
```

## Core Commands

### Chat Completions

```bash
# Basic chat
openrouter chat send "What is the capital of France?" --model openai/gpt-4o

# With system prompt
openrouter chat send "Translate to Vietnamese" --model anthropic/claude-sonnet-4 --system "You are a translator"

# JSON output (agent-friendly)
openrouter chat send "Summarize this" --model openai/gpt-4o --json --no-stream

# Streaming NDJSON
openrouter chat send "Write a haiku" --model openai/gpt-4o --output ndjson

# With temperature and max tokens
openrouter chat send "Be creative" --model openai/gpt-4o --temperature 0.9 --max-tokens 500

# Pipe input from file
cat essay.txt | openrouter chat send "Proofread this" --model anthropic/claude-sonnet-4
```

### Model Discovery

```bash
# List all models
openrouter models list

# Search models
openrouter models list --search "claude"
openrouter models list --search "gpt-4"

# Filter by provider
openrouter models list --provider anthropic

# JSON output for parsing
openrouter models list --json
```

### Credits & Usage

```bash
# Check balance
openrouter credits show

# Usage analytics
openrouter analytics show
openrouter analytics show --json
```

### API Key Management

```bash
# Auth status
openrouter auth status
openrouter auth whoami

# Manage sub-keys (requires management key)
openrouter keys list
openrouter keys create --name "my-agent" --limit 10
```

### Embeddings

```bash
# Create embeddings
openrouter embeddings create "Hello world" --model openai/text-embedding-3-small
openrouter embeddings create --file input.txt --model openai/text-embedding-3-small --json
```

### Reranking

```bash
# Rerank documents by relevance
openrouter rerank create --query "machine learning" --documents '["doc1 content", "doc2 content"]'
```

### Video Generation

```bash
# Create video
openrouter video create "A sunset over mountains" --model provider/model

# Check status and download
openrouter video status <job-id>
openrouter video wait <job-id>
openrouter video download <job-id> --output video.mp4
```

## JSON Output Format

All commands support `--json` for machine-readable output:

```json
{
  "schema_version": "1",
  "success": true,
  "data": { "..." },
  "error": null,
  "meta": { "request_id": "gen-...", "elapsed_ms": 312 }
}
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 64 | API key not set |
| 65 | Unauthorized (401) |
| 68 | Insufficient credits (402) |
| 69 | Rate limited (429) |
| 70 | Server error (5xx) |

## Configuration

```bash
# Set default model
openrouter config set defaults.model openai/gpt-4o

# Show config
openrouter config doctor --json

# Config file location
openrouter config path
```

## Popular Models

| Model | ID |
|-------|----|
| GPT-4o | `openai/gpt-4o` |
| GPT-4o Mini | `openai/gpt-4o-mini` |
| Claude Sonnet 4 | `anthropic/claude-sonnet-4` |
| Claude Haiku 3.5 | `anthropic/claude-3.5-haiku` |
| Gemini 2.5 Pro | `google/gemini-2.5-pro` |
| Llama 3.3 70B | `meta-llama/llama-3.3-70b` |
| Mistral Large | `mistralai/mistral-large` |
| DeepSeek V3 | `deepseek/deepseek-chat` |

## Tips

- Always use `--json --no-stream` when calling from scripts/agents for reliable parsing
- Use `--model` flag to specify model; set default with `openrouter config set defaults.model`
- Pipe large inputs via stdin rather than CLI args for large texts
- Check credits before heavy usage: `openrouter credits show`
