# Azure DevOps support notes

This document explains the Azure DevOps-specific divergence from `main` on the `ado` branch.

It is not a merge plan. It is a map for future maintainers so that, when pulling changes from upstream, they can quickly see:

- which parts of the codebase were changed for Azure DevOps support,
- why those changes exist,
- which parts are easy to accidentally regress during an upstream rebase or cherry-pick.

## Short version

Since `main`, this branch added a new `azure_devops` provider and then expanded it from a read-only PR sync proof of concept into a provider that also participates in middleman's clone-backed review flows.

The important part is that the work did not stay isolated to `internal/platform/azuredevops/`. Azure DevOps support required changes in:

- provider metadata and config startup,
- the provider-neutral sync engine,
- git clone auth,
- workspace/fork detection,
- server diff/files/workspace handling,
- frontend provider link generation and timeline rendering,
- activity queries and PR author display-name handling.

If upstream changes touch any of those areas, review them against this branch carefully before taking them wholesale.

## Current behavior on this branch

Compared with `main`, Azure DevOps support on this branch currently means:

- provider kind: `azure_devops`
- default host: `dev.azure.com`
- repo identity shape:
  - `owner = "ORG/PROJECT"`
  - `name = "REPO"`
  - `repo_path = "ORG/PROJECT/REPO"`
- auth for API requests: Azure CLI access token via `az account get-access-token`
- synced data:
  - repository metadata
  - open pull requests
  - PR comment threads
  - PR iteration history
- local clone-backed flows now work for Azure DevOps PRs:
  - diff SHAs
  - changed files
  - diff/file preview
  - workspace setup/review flows
- still intentionally missing:
  - Azure DevOps write mutations
  - Azure DevOps issues/work items
  - Azure DevOps CI/check ingestion

One important note: the README text added in the initial POC is now stale in one key way. It still says Azure DevOps does not support clone-backed diff/workspace flows, but later commits on this branch added that support.

## Main change areas

### 1. New provider identity and registration

Files:

- `internal/platform/types.go`
- `internal/platform/metadata.go`
- `internal/config/config.go`
- `cmd/middleman/provider_startup.go`
- `cmd/middleman/main.go`

What changed:

- Added `platform.KindAzureDevOps` and `platform.DefaultAzureDevOpsHost`.
- Added metadata so Azure DevOps is recognized as a built-in provider with nested owners enabled.
- Added kind aliases like `ado`, `azuredevops`, and `azure-devops`.
- Added config examples/comments for Azure DevOps.
- Registered the Azure DevOps provider in startup.
- Allowed Azure DevOps provider hosts to start without a stored token because auth comes from the Azure CLI on demand.

Why it exists:

Azure DevOps repository identity is not GitHub-like `owner/repo`. It is effectively `org/project/repo`, so the existing provider metadata and startup assumptions were not enough.

What to watch during upstream pulls:

- Any upstream refactor around provider metadata, `NormalizeKind`, or default-host lookup must keep the Azure aliases and `dev.azure.com` default.
- Any upstream change that assumes every configured provider host has a static token will break Azure DevOps startup.

### 2. New provider implementation

Files:

- `internal/platform/azuredevops/client.go`
- `internal/platform/azuredevops/normalize.go`
- `internal/platform/azuredevops/client_test.go`

What changed:

- Added a provider package that implements repository and merge-request reads.
- Added an auth transport that injects `Authorization: Bearer <token>`.
- Added a token source backed by `az account get-access-token`.
- Added in-memory token caching to avoid calling the Azure CLI on every request.
- Added HTTP error mapping into typed platform errors.
- Added normalization for:
  - repository metadata
  - pull requests
  - PR thread comments
  - PR iteration events
- Added Azure-specific repo parsing rules:
  - `owner` must be `ORG/PROJECT`
  - `repo_path` must be `ORG/PROJECT/REPO`

Why it exists:

This is the actual Azure DevOps adapter. It translates Azure REST responses and auth into middleman's provider-neutral model.

What to watch during upstream pulls:

- Do not lose the Azure CLI token caching layer; otherwise every API request or git auth request can shell out.
- Do not revert the repo parsing rules; they are what make `owner`, `name`, and `repo_path` coherent for Azure DevOps.
- Do not lose iteration normalization. It is the only provider-specific timeline event added here.

### 3. Sync engine changes were required, not optional

Files:

- `internal/github/sync.go`
- `internal/github/sync_test.go`

What changed:

- Extended the provider-neutral sync path so non-GitHub providers can fully sync merge-request detail through `platform.MergeRequestReader`.
- Added provider detail event persistence for Azure DevOps timeline events.
- Derived PR `CommentCount` and `LastActivityAt` from provider events after detail sync.
- Preserved `LastActivityAt` while provider detail sync is in flight so the UI does not flicker backward before events are reloaded.
- Added provider-side diff SHA updates using the local clone when head/base SHAs are available.
- Changed clone timing from eager-at-repo-start to lazy-when-needed so Azure DevOps repos only prepare clones when PR review data needs them.
- Stopped running GitHub-only conditional comment refresh logic against Azure DevOps repos.

Why it exists:

The initial provider support was not just “add a client and call it”. The sync engine had GitHub assumptions in a few places:

- comment refresh behavior,
- when clones are prepared,
- how detail sync updates derived fields,
- how diff SHAs are computed.

Without these sync changes, Azure DevOps detail sync either missed data or tried to use GitHub-only refresh paths.

What to watch during upstream pulls:

`internal/github/sync.go` is the biggest conflict hotspot. Review upstream changes carefully around:

- provider detail sync,
- comment refresh,
- clone preparation timing,
- derived field updates,
- diff SHA updates.

If an upstream sync refactor removes any of those hooks, Azure DevOps support will quietly regress even if the provider package still compiles.

### 4. Clone auth had to diverge from token-based providers

Files:

- `cmd/middleman/main.go`
- `internal/gitclone/clone.go`
- `internal/platform/azuredevops/client.go`
- `internal/gitclone/clone_security_test.go`

What changed:

- Added clone-manager support for host auth via bearer token source instead of only static basic-token auth.
- Wired Azure DevOps clone auth from `cmd/middleman/main.go` using `SetAzureBearerTokenSource`.
- Added validation so the same host is not configured for Azure DevOps and another token-based provider in a way that would require incompatible clone auth modes.
- Reduced repeated re-auth during local git operations by caching/reusing Azure CLI tokens.
- Sanitized Azure CLI failures so tokens are not leaked in error messages.

Why it exists:

Middleman's existing clone auth model assumed a static token that could be converted into git auth. Azure DevOps on this branch instead uses a bearer token fetched on demand from the Azure CLI.

What to watch during upstream pulls:

- Any upstream simplification that reduces clone auth back to “host -> static token” will break Azure DevOps diff/workspace flows.
- Keep the token-leak protections; Azure CLI failures can otherwise expose credentials in stderr/stdout handling.

### 5. Local clone, diff, and workspace flows were enabled later

Files:

- `internal/gitclone/clone.go`
- `internal/platform/local_clone.go`
- `internal/server/huma_routes.go`
- `internal/server/azure_devops_api_test.go`
- `internal/workspace/manager.go`
- `internal/workspace/monitor.go`
- `internal/github/workflow_approval.go`

What changed:

- Azure DevOps repos now go through the generic local-clone path.
- Server diff/files/file-preview/commits/workspace routes were updated to treat clone-backed features as provider-capability gated instead of implicitly GitHub-only.
- Workspace fork detection was adjusted so same-repo Azure DevOps PRs are not misclassified as fork PRs.
- Clone URL normalization now understands Azure DevOps `/_git/` paths when parsing repo identity.
- Diff SHA population for Azure DevOps PRs now uses merge-base from the local bare clone.

Why it exists:

The initial README POC was read-only sync only. Later work intentionally pushed Azure DevOps through the same review pipeline as other providers so the existing diff/files/workspace UI could work without a separate Azure-only path.

What to watch during upstream pulls:

- If upstream changes `ParseHeadRepoFullName`, `workspaceHeadRepo`, or diff route capability checks, make sure the Azure `/_git/` normalization still exists.
- If upstream adds provider-specific local-clone gating, Azure DevOps must remain enabled or the later branch work is lost.

### 6. Git environment handling changed to support safe clone/workspace auth

Files:

- `internal/gitenv/gitenv.go`
- `internal/workspace/manager.go`
- `internal/workspace/monitor.go`

What changed:

- Added centralized git environment scrubbing for child git processes.
- Added `NullConfigPath()` so Windows does not get broken `NUL` config paths in `GIT_CONFIG_GLOBAL`.
- Ensured clone/workspace commands run with isolated git config injection.

Why it exists:

Some of this work was prompted by Azure clone auth, but it also hardens all child git usage. Without it, injected git config and inherited `GIT_*` variables can make clone/workspace behavior flaky or unsafe.

What to watch during upstream pulls:

This is adjacent infrastructure, but it matters for Azure review flows. If upstream reworks git subprocess spawning, keep the env stripping and Windows-safe null config behavior.

### 7. Server and API coverage expanded for Azure route shapes

Files:

- `internal/server/huma_routes.go`
- `internal/server/azure_devops_api_test.go`
- `internal/server/api_test.go`

What changed:

- Added end-to-end coverage for Azure DevOps PR sync, detail loading, iterations, and diff-backed endpoints.
- Exercised provider-aware routes using Azure's nested owner shape, for example:
  - `/api/v1/pulls/azure_devops/AcmeOrg%2FPayments/Service/17`
- Added tests around repo paths containing spaces.

Why it exists:

Azure DevOps stresses the route model in two ways:

- `owner` itself contains a slash (`ORG/PROJECT`), so it must be encoded once.
- repo/org/project names may contain spaces, so path-segment encoding must also be correct.

What to watch during upstream pulls:

- Do not replace shared route helpers or request builders with ad hoc string joins.
- Be careful with any upstream route or escaping refactor; Azure DevOps is the easiest provider to break there.

### 8. Frontend got Azure-specific external links and timeline rendering

Files:

- `packages/ui/src/api/provider-links.ts`
- `packages/ui/src/api/provider-links.test.ts`
- `packages/ui/src/components/detail/EventTimeline.svelte`
- `packages/ui/src/components/detail/EventTimeline.test.ts`
- `packages/ui/src/components/diff/DiffFile.svelte`
- `packages/ui/src/components/diff/DiffFile.test.ts`
- `frontend/src/lib/components/repositories/repoSummary.ts`
- `frontend/src/lib/components/repositories/RepoSummaryCard.svelte`
- `frontend/src/lib/components/repositories/repoSummary.test.ts`

What changed:

- Added provider-specific external URL generation for Azure DevOps repos, PR comments, and changed files.
- Azure DevOps repo URLs use the `/_git/` path segment.
- PR comment deep links use Azure's `discussionId` query parameter.
- Changed-file deep links use Azure's `?_a=files&path=...` format.
- Event timeline now renders Azure iteration events.
- Repo summary cards were updated so Azure repos link out correctly.

Why it exists:

The neutral API stores provider refs, but the browser still needs provider-specific external URLs when the user wants to jump from middleman to the provider.

What to watch during upstream pulls:

- If upstream changes provider-link helpers, keep the Azure `/_git/` URL rules.
- If upstream changes timeline rendering, do not drop the `iteration` event type.
- If upstream changes repo summary cards, keep them using the shared provider link helper instead of reconstructing GitHub-style URLs.

### 9. Activity feed and PR author presentation were adjusted for Azure data

Files:

- `internal/db/queries_activity.go`
- `internal/db/queries_activity_test.go`
- `internal/server/api_test.go`
- `packages/ui/src/components/ActivityFeed.svelte`
- `packages/ui/src/components/ActivityThreaded.svelte`
- `packages/ui/src/views/MobileActivityView.svelte`
- `packages/ui/src/components/detail/PullDetail.svelte`
- `packages/ui/src/components/sidebar/PullItem.svelte`
- `packages/ui/src/components/kanban/KanbanCard.svelte`
- `packages/ui/src/utils/bot-authors.ts`
- `packages/ui/src/components/detail/prTimelineFilter.ts`

What changed:

- Activity queries now include `iteration` events from PR timelines.
- PR-related UI now prefers `author_display_name` where present, which matters for Azure service accounts whose canonical author may be an email-like unique name.
- Added bot filtering tweaks so Azure/automation-heavy activity is less noisy in the feed and timeline.
- Repo/activity UI surfaces were updated to keep the Azure author identity readable.

Why it exists:

Azure identity data often has both a machine-usable unique name and a human display name. Showing only the canonical author string made some PR/activity views noticeably worse.

What to watch during upstream pulls:

- Keep `iteration` in activity SQL and UI filters.
- Keep display-name preference in PR cards/detail/feed unless upstream introduces a better provider-neutral author model.

## Specific edge cases this branch had to fix

These were all real follow-up fixes after the initial provider landed:

- Azure sync was accidentally using GitHub comment-refresh behavior.
- Provider detail sync could briefly regress `LastActivityAt` until events were reloaded.
- Clone-backed review initially did not have the right auth model for Azure.
- Azure CLI auth failures could leak tokens in error strings.
- Local git commands were reauthing too often.
- Windows git config handling needed a real empty config file instead of `NUL`.
- Repo/org/project names containing spaces needed single-encoding, not double-encoding.
- Timeline/deep-link UI needed Azure-specific URL rules.
- Some PR/activity UI needed display-name fallbacks for Azure identities.
- Summary cards needed provider-aware repo links restored for Azure.

Those fixes are why the final divergence is spread across many files rather than only the provider package.

## Conflict hotspots when pulling from upstream

If upstream touched these files or areas, review manually:

1. `internal/github/sync.go`
   - highest-risk file
   - provider detail sync, clone timing, comment refresh, derived fields

2. `internal/gitclone/clone.go`
   - Azure bearer-token clone auth lives here

3. `cmd/middleman/main.go` and `cmd/middleman/provider_startup.go`
   - tokenless Azure startup and clone-auth wiring live here

4. `internal/workspace/manager.go` and `internal/github/workflow_approval.go`
   - Azure `/_git/` repo identity normalization affects workspaces and workflow matching

5. `internal/server/huma_routes.go`
   - clone-backed endpoint gating and provider-aware route handling

6. `packages/ui/src/api/provider-links.ts`
   - Azure external URL shape lives here

7. `internal/db/queries_activity.go`
   - iteration events and author presentation can regress here

## Commit-by-commit summary since `main`

In chronological order:

- `f0419f0` `build: add nix shell for local builds`
  - unrelated to Azure support
- `c2c5ccf` `feat: add a read-only Azure DevOps PR provider`
  - initial provider, startup wiring, config/docs, server e2e coverage
- `d892f1e` `fix: stop Azure DevOps sync from using GitHub comment refresh`
  - removed GitHub-only refresh assumption from provider sync
- `1ce489e` `fix: keep PR activity stable during provider detail sync`
  - preserved activity fields while provider events refresh
- `4952fba` `feat: enable Azure DevOps clone-backed PR review`
  - local clone, diff, workspace, workflow URL parsing, server support
- `0fd5861` `fix: resolve Azure clone auth constructor compile error`
  - follow-up fix in clone auth plumbing
- `a33b951` `fix: avoid NUL git config paths on Windows`
  - git subprocess env/config hardening for clone/workspace flows
- `14f2e4e` `fix: stop Azure CLI clone auth errors from leaking tokens`
  - auth error sanitization
- `c3d6dc1` `fix: stop Azure diffs from reauthing every local git command`
  - token reuse/caching improvements for local git flows
- `a68e828` `feat: add Azure DevOps deep links for comments and files`
  - frontend provider links and timeline/diff UI
- `dc1e105` `fix: place timeline deep-link const in a valid Svelte block`
  - follow-up Svelte correctness fix
- `8e2e2e5` `fix: sync Azure DevOps repos whose paths contain spaces`
  - path encoding fixes in provider/server tests
- `7e4d445` `feat: show Azure DevOps PR iteration history`
  - iteration normalization and timeline UI
- `dec550b` `fix: include Azure DevOps iterations in activity feeds`
  - activity SQL/UI support for iterations
- `e8bfa18` `fix: hide Agency and Copilot as bot activity`
  - bot filtering polish
- `cd6528d` `fix: prefer Azure DevOps PR display names for service accounts`
  - display-name presentation fixes across UI/API activity
- `b484866` `fix: restore activity query after PR display-name fallback`
  - follow-up DB query fix
- `865f350` `fix: restore Azure DevOps repo links on summary cards`
  - repo summary external-link fix

## Practical guidance for future merges

If the goal is to pull upstream changes without losing Azure support, treat the Azure branch work as four themes:

1. provider registration and normalization
2. sync engine/provider detail behavior
3. clone/workspace auth and repo identity parsing
4. frontend/provider-link/timeline/activity polish

Even if upstream already has adjacent work in one of those areas, do not assume the Azure behavior will survive a clean textual merge. The provider package alone is not enough.
