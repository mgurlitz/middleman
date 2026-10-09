# Azure DevOps support

The `ado` branch adds read-only Azure DevOps pull requests to Kenn Forge.
Authenticate with `az login`; Forge obtains and caches Azure access tokens for
API reads and authenticated Git operations.

## Configure a repository

```toml
[[repos]]
platform = "azure_devops"
owner = "ExampleOrg/ExampleProject"
name = "ExampleRepo"
repo_path = "ExampleOrg/ExampleProject/ExampleRepo"
```

The organization and project together form the owner. Names containing spaces
are supported. Repository identity follows Azure's native repository UUID,
not its mutable organization/project/repository route.

## Available workflows

- Sync pull requests, comments, approvals, and iteration history.
- Review changed files, diffs, and file previews from local Git clones.
- Create pull-request workspaces and follow links to Azure repositories,
  comments, and changed files.

Azure write operations, work items, and CI ingestion are not implemented.
Approval events describe upstream activity; they do not enable approval actions.

## Maintainer constraints

Keep Azure CLI authentication available to both provider reads and managed Git.
Git uses a host-scoped bearer header, not Basic authentication. Preserve
credential rotation and error sanitization when updating token handling.

Azure HTTPS Git routes contain `/_git/`; normalize that transport marker only
when checking repository identity, never when choosing the execution URL.
Owner and project segments must be encoded exactly once.

The adapter participates in ordinary provider sync and persistence. Do not
restore superseded sync routines or the retired `packages/ui` application tree
when bringing in older Azure commits; current UI components live in `frontend`.

Back up an existing installation before upgrading its database. This build
refuses to migrate older databases containing Azure repositories because the
upstream integer-ID migration would delete their UUID-backed cache rows.
Keep using the previous build with that database until a dedicated UUID upgrade
is available. New databases support Azure UUIDs.
