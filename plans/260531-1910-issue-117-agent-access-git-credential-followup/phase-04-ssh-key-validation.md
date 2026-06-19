---
phase: 4
title: ssh-key-validation
status: completed
effort: M
---

# Phase 4: ssh-key-validation

## Overview

Catch OpenSSH-incompatible private keys before they are stored.

## Implementation Steps

1. Add a storage-oriented validator in `internal/tools` that first calls
   `ValidateSSHKey`, then checks OpenSSH compatibility via `ssh-keygen -y -f`
   on a 0600 temporary file.
2. If `ssh-keygen` is unavailable, keep the Go parser result rather than
   breaking non-OpenSSH dev environments.
3. Route typed credential saves through the new storage validator in
   `internal/http/secure_cli_typed_credentials.go`.
4. Return the existing git credential validation error shape so UI handling
   does not need a new API family.
5. Keep runtime validation in `gitAdapter.Prepare` lightweight; do not spawn
   `ssh-keygen` on every command.

## Success Criteria

- [ ] Save-time SSH validation test proves OpenSSH incompatibility is surfaced
      before encryption/persistence.
- [ ] Valid unencrypted private keys still pass.
- [ ] Passphrase-protected keys remain rejected.
- [ ] No key material is logged or returned in validation errors.
