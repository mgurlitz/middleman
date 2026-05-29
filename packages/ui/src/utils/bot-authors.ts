const BOT_SUFFIXES = ["[bot]", "-bot", "bot"];
const BOT_NAMES = new Set(["agency", "github copilot"]);

export function isBotAuthor(author: string): boolean {
  const normalized = author.trim().toLowerCase();
  if (normalized === "") return false;
  return BOT_NAMES.has(normalized)
    || BOT_SUFFIXES.some((suffix) => normalized.endsWith(suffix));
}
