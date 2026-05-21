import type { DiffFile, IssueEvent, PREvent } from "./types.js";
import type { ProviderRouteRef } from "./provider-routes.js";

export type TimelineItemType = "pull" | "issue";

type TimelineEvent = PREvent | IssueEvent;

function canonicalProvider(provider: string | null | undefined): string {
  const normalized = provider?.trim().toLowerCase() ?? "";
  if (normalized === "gh") return "github";
  if (normalized === "gl") return "gitlab";
  if (
    normalized === "ado"
    || normalized === "azuredevops"
    || normalized === "azure-devops"
  ) {
    return "azure_devops";
  }
  return normalized;
}

function providerHost(ref: ProviderRouteRef): string {
  const host = ref.platformHost?.trim();
  if (host) return host;
  switch (canonicalProvider(ref.provider)) {
    case "azure_devops":
      return "dev.azure.com";
    case "gitlab":
      return "gitlab.com";
    case "github":
    default:
      return "github.com";
  }
}

function joinPathSegments(...segments: string[]): string {
  return segments
    .flatMap((segment) => segment.split("/"))
    .map((segment) => encodeURIComponent(segment.trim()))
    .join("/");
}

function azureRepoBaseURL(ref: ProviderRouteRef): string {
  return `https://${providerHost(ref)}/${joinPathSegments(ref.owner)}/_git/${encodeURIComponent(ref.name)}`;
}

export function providerCommentURL(
  ref: ProviderRouteRef,
  itemType: TimelineItemType,
  itemNumber: number | undefined,
  event: TimelineEvent,
): string | null {
  if (itemNumber == null) return null;
  if (canonicalProvider(ref.provider) !== "azure_devops") return null;
  if (itemType !== "pull") return null;
  if (event.EventType !== "issue_comment") return null;
  const externalID = event.PlatformExternalID?.trim() ?? "";
  if (externalID === "") return null;
  const [threadID] = externalID.split(":", 2);
  const prURL = `${azureRepoBaseURL(ref)}/pullrequest/${itemNumber}`;
  if (threadID === "") return prURL;
  return `${prURL}?_a=overview&discussionId=${encodeURIComponent(threadID)}`;
}

export function providerDiffFileURL(
  ref: ProviderRouteRef,
  itemNumber: number,
  file: Pick<DiffFile, "path">,
): string | null {
  if (canonicalProvider(ref.provider) !== "azure_devops") return null;
  const normalizedPath = file.path.startsWith("/") ? file.path : `/${file.path}`;
  return `${azureRepoBaseURL(ref)}/pullrequest/${itemNumber}?_a=files&path=${encodeURIComponent(normalizedPath)}`;
}
