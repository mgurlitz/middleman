import { render, screen, cleanup } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { PullRequest } from "../../api/types.js";
import KanbanCard from "./KanbanCard.svelte";

function pull(overrides: Record<string, unknown> = {}): PullRequest {
  return {
    Number: 7,
    Title: "Build service maintenance",
    Author: "svc-principal-1234",
    AuthorDisplayName: "Acme Build Service",
    LastActivityAt: "2026-05-01T12:00:00Z",
    repo: {
      name: "widgets",
    },
    ...overrides,
  } as unknown as PullRequest;
}

describe("KanbanCard", () => {
  afterEach(() => {
    cleanup();
  });

  it("prefers AuthorDisplayName over the raw author id", () => {
    render(KanbanCard, {
      props: {
        pr: pull(),
        onclick: vi.fn(),
      },
    });

    expect(screen.getByText("Acme Build Service")).toBeTruthy();
    expect(screen.queryByText("svc-principal-1234")).toBeNull();
  });
});
