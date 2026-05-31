# Red-team Report

## Result

Approved for implementation with two guardrails.

## Findings

1. Basic auth creates a second secret representation. Fix must scrub the raw PAT,
   the base64 payload, and the full header value. Otherwise logs can hide
   `ghp_...` while leaking `eC1hY2Nlc3MtdG9rZW46...`.
2. Do not run `ssh-keygen` on every agent git command. Runtime command execution
   is hot path and already has key materialization. OpenSSH compatibility belongs
   at save time.
3. Avoid nested Radix dialogs. The Agent Access implementation must extract
   content bodies or keep one dialog root; rendering old dialog components inside
   a new wrapper would keep the overlap class of bug.
4. Keep credential precedence unchanged. The production evidence shows agent
   credentials are selected; changing resolver precedence would be a regression.

## Decisions

- Plan already includes scrub expansion in phase 3.
- Plan already constrains OpenSSH validation to storage path in phase 4.
- Plan phase 2 explicitly requires content extraction instead of nested dialogs.
- No blocker remains.

## Unresolved Questions

None.
