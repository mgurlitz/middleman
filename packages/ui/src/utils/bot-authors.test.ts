import { describe, expect, it } from "vitest";
import { isBotAuthor } from "./bot-authors.js";

describe("isBotAuthor", () => {
  it("matches suffix-based bot names", () => {
    expect(isBotAuthor("renovate[bot]")).toBe(true);
    expect(isBotAuthor("merge-bot")).toBe(true);
    expect(isBotAuthor("somebot")).toBe(true);
  });

  it("matches exact bot names that do not end with bot", () => {
    expect(isBotAuthor("Agency")).toBe(true);
    expect(isBotAuthor("GitHub Copilot")).toBe(true);
    expect(isBotAuthor(" github copilot ")).toBe(true);
  });

  it("does not mark ordinary human names as bots", () => {
    expect(isBotAuthor("Ada Lovelace")).toBe(false);
    expect(isBotAuthor("Alice")).toBe(false);
  });
});
