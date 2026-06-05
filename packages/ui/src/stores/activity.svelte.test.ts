import { beforeEach, describe, expect, it } from "vitest";
import type { ActivitySettings } from "../api/types.js";
import { createActivityStore } from "./activity.svelte.js";

const fakeClient = {
  GET: async () => ({ data: { items: [], capped: false }, error: null }),
} as unknown as Parameters<typeof createActivityStore>[0]["client"];

function settings(
  collapse: boolean,
  overrides: Partial<Pick<ActivitySettings, "hide_closed" | "hide_bots">> = {},
): ActivitySettings {
  return {
    view_mode: "threaded",
    time_range: "7d",
    hide_closed: overrides.hide_closed ?? false,
    hide_bots: overrides.hide_bots ?? false,
    collapse_threads: collapse,
  };
}

function makeStore() {
  return createActivityStore({ client: fakeClient });
}

beforeEach(() => {
  window.history.replaceState(null, "", "/");
});

describe("activity store collapse state", () => {
  it("treats threads as expanded when the collapse default is false", () => {
    const s = makeStore();
    s.hydrateDefaults(settings(false));
    expect(s.getCollapseThreads()).toBe(false);
    expect(s.isThreadItemExpanded("k1")).toBe(true);
  });

  it("collapseAllThreads collapses everything and clears overrides", () => {
    const s = makeStore();
    s.hydrateDefaults(settings(false));
    s.toggleThreadItem("k1");
    expect(s.isThreadItemExpanded("k1")).toBe(false);
    s.collapseAllThreads();
    expect(s.getCollapseThreads()).toBe(true);
    expect(s.isThreadItemExpanded("k1")).toBe(false);
    expect(s.isThreadItemExpanded("k2")).toBe(false);
  });

  it("toggleThreadItem expands a single item when globally collapsed", () => {
    const s = makeStore();
    s.hydrateDefaults(settings(true));
    expect(s.isThreadItemExpanded("k1")).toBe(false);
    s.toggleThreadItem("k1");
    expect(s.isThreadItemExpanded("k1")).toBe(true);
    expect(s.isThreadItemExpanded("k2")).toBe(false);
  });

  it("toggleThreadItem twice returns an item to the global state", () => {
    const s = makeStore();
    s.hydrateDefaults(settings(false));
    s.toggleThreadItem("k1");
    expect(s.isThreadItemExpanded("k1")).toBe(false);
    s.toggleThreadItem("k1");
    expect(s.isThreadItemExpanded("k1")).toBe(true);
  });

  it("writes collapsed to the URL only when it differs from the server default", () => {
    const s = makeStore();
    s.hydrateDefaults(settings(false));
    s.collapseAllThreads();
    expect(new URLSearchParams(window.location.search).get("collapsed")).toBe("1");
    s.expandAllThreads();
    expect(new URLSearchParams(window.location.search).has("collapsed")).toBe(false);
  });

  it("writes collapsed=0 when expanding against a collapsed server default", () => {
    const s = makeStore();
    s.hydrateDefaults(settings(true));
    s.expandAllThreads();
    expect(new URLSearchParams(window.location.search).get("collapsed")).toBe("0");
    s.collapseAllThreads();
    expect(new URLSearchParams(window.location.search).has("collapsed")).toBe(false);
  });

  it("applies collapsed=0 from the URL over a collapsed server default", () => {
    window.history.replaceState(null, "", "/?collapsed=0");
    const s = makeStore();
    s.hydrateDefaults(settings(true));
    s.initializeFromMount();
    expect(s.getCollapseThreads()).toBe(false);
  });

  it("preserves a live collapsed override when settings reload after init", () => {
    window.history.replaceState(null, "", "/?collapsed=0");
    const s = makeStore();
    s.hydrateDefaults(settings(true));
    s.initializeFromMount();
    expect(s.getCollapseThreads()).toBe(false);
    s.hydrateDefaults(settings(true));
    expect(s.getCollapseThreads()).toBe(false);
  });

  it("clears a redundant collapsed param when the default catches up to it", () => {
    window.history.replaceState(null, "", "/?collapsed=1");
    const s = makeStore();
    s.hydrateDefaults(settings(false));
    s.initializeFromMount();
    expect(s.getCollapseThreads()).toBe(true);
    expect(new URLSearchParams(window.location.search).get("collapsed")).toBe("1");

    // The server default changes to match the live override; the now-redundant
    // param is dropped so a later default change is not shadowed by it.
    s.hydrateDefaults(settings(true));
    expect(s.getCollapseThreads()).toBe(true);
    expect(new URLSearchParams(window.location.search).has("collapsed")).toBe(false);

    s.hydrateDefaults(settings(false));
    expect(s.getCollapseThreads()).toBe(false);
  });
});

describe("activity store visibility persistence", () => {
  it("shows default-branch activity by default and persists the hide flag", () => {
    const s = makeStore();
    s.initializeFromMount();
    expect(s.getHideDefaultBranchActivity()).toBe(false);

    s.setHideDefaultBranchActivity(true);
    s.syncToURL();
    expect(new URLSearchParams(window.location.search).get("hide_branch")).toBe("1");

    const next = makeStore();
    next.initializeFromMount();
    expect(next.getHideDefaultBranchActivity()).toBe(true);

    next.setHideDefaultBranchActivity(false);
    next.syncToURL();
    expect(new URLSearchParams(window.location.search).has("hide_branch")).toBe(false);
  });

  it("persists hide closed/merged across reloads", () => {
    const s = makeStore();
    s.initializeFromMount();
    expect(s.getHideClosedMerged()).toBe(false);

    s.setHideClosedMerged(true);
    s.syncToURL();
    expect(new URLSearchParams(window.location.search).get("hide_closed")).toBe("1");

    const next = makeStore();
    next.initializeFromMount();
    expect(next.getHideClosedMerged()).toBe(true);

    next.setHideClosedMerged(false);
    next.syncToURL();
    expect(new URLSearchParams(window.location.search).has("hide_closed")).toBe(false);
  });

  it("persists hide closed/merged false overrides when the default is true", () => {
    const s = makeStore();
    s.hydrateDefaults(settings(false, { hide_closed: true }));
    s.initializeFromMount();
    expect(s.getHideClosedMerged()).toBe(true);

    s.setHideClosedMerged(false);
    s.syncToURL();
    expect(new URLSearchParams(window.location.search).get("hide_closed")).toBe("0");

    const next = makeStore();
    next.hydrateDefaults(settings(false, { hide_closed: true }));
    next.initializeFromMount();
    expect(next.getHideClosedMerged()).toBe(false);
  });

  it("persists hide bots across reloads", () => {
    const s = makeStore();
    s.initializeFromMount();
    expect(s.getHideBots()).toBe(false);

    s.setHideBots(true);
    s.syncToURL();
    expect(new URLSearchParams(window.location.search).get("hide_bots")).toBe("1");

    const next = makeStore();
    next.initializeFromMount();
    expect(next.getHideBots()).toBe(true);

    next.setHideBots(false);
    next.syncToURL();
    expect(new URLSearchParams(window.location.search).has("hide_bots")).toBe(false);
  });

  it("persists hide bots false overrides when the default is true", () => {
    const s = makeStore();
    s.hydrateDefaults(settings(false, { hide_bots: true }));
    s.initializeFromMount();
    expect(s.getHideBots()).toBe(true);

    s.setHideBots(false);
    s.syncToURL();
    expect(new URLSearchParams(window.location.search).get("hide_bots")).toBe("0");

    const next = makeStore();
    next.hydrateDefaults(settings(false, { hide_bots: true }));
    next.initializeFromMount();
    expect(next.getHideBots()).toBe(false);
  });
});
