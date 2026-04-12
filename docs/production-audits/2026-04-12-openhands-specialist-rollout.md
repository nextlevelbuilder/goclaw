# OpenHands Specialist Rollout Audit

Date: 2026-04-12

## Outcome

The OpenHands specialist integration is live and usable from GoClaw production.

Public endpoints:

- GoClaw: `https://goclaw.blackbirdzzzz.art`
- OpenHands adapter: `https://openhands.blackbirdzzzz.art`

## Final production shape

- dedicated OpenHands adapter runtime on `linuxvm`
- public Blackbird adapter in front of the raw OpenHands workspace server
- GoClaw production configured with `GOCLAW_OPENHANDS_ENABLED=1`
- predefined specialist agent `openhands-coder`
- native tool `openhands_delegate` available in production agent inventory

## Key fixes during rollout

1. fixed unpublished git handoff handling in `openhands_delegate`
2. installed the correct Docker CLI package inside the adapter image
3. routed workspace health checks through `host.docker.internal`
4. removed unsupported `persistence_dir` from remote conversations
5. normalized OpenHands conversation tags to valid alphanumeric keys
6. switched adapter LLM runtime to LiteLLM OpenAI-compatible mode:
   `openai/claude-opus-4.6` via `https://llm.chiasegpu.vn/v1`

## Verification evidence

Direct adapter smoke:

- job id: `d5006e8b-9be1-4c3b-9506-8ba2085ce22d`
- source: direct adapter API
- result: succeeded
- changed file: `SMOKE_OPENHANDS.txt`

GoClaw end-to-end smoke:

- job id: `8f0da4d7-7c92-420c-978c-443f1a919f64`
- source agent: `openhands-coder`
- result: succeeded
- changed file: `GOCLAW_PROBE.txt`

## Residual notes

- Linux VM shell DNS resolution for the public hostname is not the primary health
  signal; local service checks should use `http://127.0.0.1:8091/health`
- LiteLLM warns that `claude-opus-4.6` pricing metadata is unmapped, but the
  model runs correctly through the configured gateway
