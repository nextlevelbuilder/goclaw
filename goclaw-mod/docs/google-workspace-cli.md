# Google Workspace CLI Integration

GoClaw supports Google Workspace CLI through the `gws` binary from `@googleworkspace/cli`.

## Runtime availability

- Published `full` image: `gws` is preinstalled.
- Published `latest` image: Python is available, but Node/npm and `gws` are not preinstalled.
- Published `base` image: Python and Node/npm are not preinstalled.
- Custom Node-enabled builds: set `ENABLE_NODE=true`, then install `npm:@googleworkspace/cli` from the Packages page or `/v1/packages/install`.

`gws` requires Node.js 18+ when installed through npm.

## SecureCLI setup

Create a SecureCLI credential from the `gws` preset. Provide at least one usable auth source:

- `GOOGLE_WORKSPACE_CLI_CREDENTIALS_FILE`: exported `gws` credentials or an OAuth credentials JSON file path.
- `GOOGLE_WORKSPACE_CLI_TOKEN`: pre-obtained OAuth access token.
- `GOOGLE_WORKSPACE_CLI_CLIENT_ID` and `GOOGLE_WORKSPACE_CLI_CLIENT_SECRET`: optional OAuth client values for external auth flows.

The preset blocks these interactive or credential-exporting commands:

- `gws auth setup`
- `gws auth login`
- `gws auth export`
- `gws auth logout`

Run those flows outside agent execution, then store the resulting token or credentials file path in SecureCLI.

## Agent command patterns

Use `--params` for query parameters and `--json` for request bodies. Prefer read-only commands unless an admin has explicitly approved writes.

Drive:

```sh
gws drive files list --params '{"pageSize": 10}'
```

Gmail:

```sh
gws gmail users messages list --params '{"userId": "me", "maxResults": 10}'
```

Calendar:

```sh
gws calendar events list --params '{"calendarId": "primary", "maxResults": 10}'
```

Pagination:

```sh
gws drive files list --params '{"pageSize": 100}' --page-all
```

Schema inspection:

```sh
gws schema drive.files.list
```

## Validation

Without credentials, local validation can only prove packaging and credential injection:

```sh
gws --help
gws drive files list --help
```

With credentials available, run these smoke tests from a SecureCLI-enabled agent or equivalent runtime:

```sh
gws drive files list --params '{"pageSize": 1}'
gws gmail users messages list --params '{"userId": "me", "maxResults": 1}'
gws calendar events list --params '{"calendarId": "primary", "maxResults": 1}'
```

Do not mark live Google Workspace validation complete unless all three authenticated commands return successful JSON.

## Limitations

- Google Workspace auth and scopes are controlled by the configured Google account, OAuth app, token, or credentials file.
- Domain-wide delegation and account impersonation are not represented by a GoClaw preset env var. Configure those in Google Workspace and the credential file if needed.
- Write commands can modify Workspace data. Keep the default preset read-oriented, and create a separate reviewed SecureCLI config for approved write workflows.
