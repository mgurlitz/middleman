import type { DiffFile, IssueEvent, PREvent } from "./types.js";
import { canonicalProvider, providerDefaultHost, type ProviderRouteRef } from "./provider-routes.js";

export type TimelineItemType = "pull" | "issue";

type TimelineEvent = PREvent | IssueEvent;

function providerHost(ref: ProviderRouteRef): string {
  return ref.platformHost?.trim() || providerDefaultHost(ref.provider) || "";
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

export function providerRepoURL(ref: ProviderRouteRef): string {
  if (canonicalProvider(ref.provider) === "azure_devops") {
    return azureRepoBaseURL(ref);
  }
  return `https://${providerHost(ref)}/${joinPathSegments(ref.owner, ref.name)}`;
}

export function providerCommentURL(
  ref: ProviderRouteRef,
  itemType: TimelineItemType | undefined,
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
  if (!threadID) return prURL;
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
