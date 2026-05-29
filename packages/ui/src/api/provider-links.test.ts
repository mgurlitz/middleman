import { describe, expect, it } from "vitest";
import { providerCommentURL, providerDiffFileURL, providerRepoURL } from "./provider-links.js";
import type { PREvent } from "./types.js";

function makeEvent(overrides: Partial<PREvent> = {}): PREvent {
  return {
    ID: 1,
    MergeRequestID: 42,
    PlatformID: 1001,
    PlatformExternalID: "55:1001",
    EventType: "issue_comment",
    Author: "grace@example.com",
    Body: "Looks good",
    Summary: "",
    MetadataJSON: "",
    CreatedAt: "2026-05-21T12:05:00Z",
    DedupeKey: "event-1",
    ...overrides,
  } as PREvent;
}

describe("provider links", () => {
  it("builds Azure DevOps comment links for PR discussion threads", () => {
    expect(providerCommentURL(
      {
        provider: "azure_devops",
        platformHost: "dev.azure.com",
        owner: "AcmeOrg/Payments",
        name: "Service",
        repoPath: "AcmeOrg/Payments/Service",
      },
      "pull",
      17,
      makeEvent(),
    )).toBe(
      "https://dev.azure.com/AcmeOrg/Payments/_git/Service/pullrequest/17?_a=overview&discussionId=55",
    );
  });

  it("builds Azure DevOps file links for changed files", () => {
    expect(providerDiffFileURL(
      {
        provider: "azure_devops",
        platformHost: "dev.azure.com",
        owner: "AcmeOrg/Payments",
        name: "Service",
        repoPath: "AcmeOrg/Payments/Service",
      },
      17,
      { path: "src/handler.go" },
    )).toBe(
      "https://dev.azure.com/AcmeOrg/Payments/_git/Service/pullrequest/17?_a=files&path=%2Fsrc%2Fhandler.go",
    );
  });

  it("builds Azure DevOps repo links with the _git path segment", () => {
    expect(providerRepoURL({
      provider: "azure_devops",
      platformHost: "dev.azure.com",
      owner: "AcmeOrg/Payments",
      name: "Service",
      repoPath: "AcmeOrg/Payments/Service",
    })).toBe(
      "https://dev.azure.com/AcmeOrg/Payments/_git/Service",
    );
  });

  it("returns null for non-Azure providers", () => {
    expect(providerCommentURL(
      {
        provider: "github",
        platformHost: "github.com",
        owner: "acme",
        name: "widget",
        repoPath: "acme/widget",
      },
      "pull",
      17,
      makeEvent(),
    )).toBeNull();
    expect(providerDiffFileURL(
      {
        provider: "github",
        platformHost: "github.com",
        owner: "acme",
        name: "widget",
        repoPath: "acme/widget",
      },
      17,
      { path: "src/handler.go" },
    )).toBeNull();
  });
});
