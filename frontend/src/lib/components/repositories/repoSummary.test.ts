import { describe, expect, it } from "vite-plus/test";
import type { RepoSummary } from "@middleman/ui/api/types";

import {
  defaultProviderCapabilities,
  externalRepoURL,
  normalizeSummaries,
  repoKey,
  shouldShowPlatformHost,
} from "./repoSummary.js";

describe("repo summary labels", () => {
  it("hides github.com when it is the default platform host", () => {
    const summary = {
      platform_host: "github.com",
      default_platform_host: "github.com",
      owner: "acme",
      name: "widgets",
    };

    expect(shouldShowPlatformHost(summary)).toBe(false);
    expect(repoKey(summary)).toBe("acme/widgets");
  });

  it("hides a configured non-github default platform host", () => {
    const summary = {
      platform_host: "ghe.example.com",
      default_platform_host: "ghe.example.com",
      owner: "acme",
      name: "widgets",
    };

    expect(shouldShowPlatformHost(summary)).toBe(false);
    expect(repoKey(summary)).toBe("acme/widgets");
  });

  it("includes a non-default platform host in repository labels", () => {
    const summary = {
      platform_host: "ghe.example.com",
      default_platform_host: "github.com",
      owner: "acme",
      name: "widgets",
    };

    expect(shouldShowPlatformHost(summary)).toBe(true);
    expect(repoKey(summary)).toBe("ghe.example.com/acme/widgets");
  });

  it("builds Azure DevOps external repo links with the _git segment", () => {
    const [summary] = normalizeSummaries([{
      owner: "AcmeOrg/Payments",
      name: "Service",
      platform_host: "dev.azure.com",
      repo: {
        provider: "azure_devops",
        platform_host: "dev.azure.com",
        owner: "AcmeOrg/Payments",
        name: "Service",
        repo_path: "AcmeOrg/Payments/Service",
      },
      default_platform_host: "dev.azure.com",
      cached_pr_count: 0,
      open_pr_count: 0,
      draft_pr_count: 0,
      cached_issue_count: 0,
      open_issue_count: 0,
      active_authors: null,
      recent_issues: null,
      commit_timeline: null,
      releases: null,
    } as unknown as RepoSummary]);

    expect(summary).toBeDefined();
    if (!summary) throw new Error("summary missing");
    expect(externalRepoURL(summary)).toBe(
      "https://dev.azure.com/AcmeOrg/Payments/_git/Service",
    );
  });

  it("defaults missing repo capabilities when provider identity is present", () => {
    const [summary] = normalizeSummaries([
      {
        owner: "acme",
        name: "widgets",
        platform_host: "github.com",
        repo: {
          provider: "github",
          platform_host: "github.com",
          owner: "acme",
          name: "widgets",
          repo_path: "acme/widgets",
        },
        default_platform_host: "github.com",
        cached_pr_count: 0,
        open_pr_count: 0,
        draft_pr_count: 0,
        cached_issue_count: 0,
        open_issue_count: 0,
        active_authors: null,
        recent_issues: null,
        commit_timeline: null,
        releases: null,
      } as unknown as RepoSummary,
    ]);

    expect(summary).toBeDefined();
    if (!summary) throw new Error("summary missing");
    expect(summary.repo.capabilities).toEqual(defaultProviderCapabilities);
    expect(summary.repo.repo_path).toBe("acme/widgets");
  });

  it("preserves provider capability flags from the API", () => {
    const [summary] = normalizeSummaries([
      {
        owner: "group",
        name: "project",
        platform_host: "gitlab.com",
        default_platform_host: "github.com",
        cached_pr_count: 0,
        open_pr_count: 0,
        draft_pr_count: 0,
        cached_issue_count: 0,
        open_issue_count: 0,
        active_authors: null,
        recent_issues: null,
        commit_timeline: null,
        releases: null,
        repo: {
          provider: "gitlab",
          platform_host: "gitlab.com",
          repo_path: "group/project",
          owner: "group",
          name: "project",
          capabilities: {
            ...defaultProviderCapabilities,
            issue_mutation: false,
          },
        },
      } as unknown as RepoSummary,
    ]);

    expect(summary).toBeDefined();
    if (!summary) throw new Error("summary missing");
    expect(summary.repo.capabilities.issue_mutation).toBe(false);
  });
});
