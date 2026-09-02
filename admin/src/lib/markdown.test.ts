import { describe, expect, it } from "vitest";
import { parseBlocks } from "./markdown";

// The block grammar behind the chat renderer. The rendering itself is plain
// React elements (no HTML strings anywhere), so the parser is the part whose
// regressions would leak syntax into bubbles.
describe("parseBlocks", () => {
  it("splits paragraphs on blank lines", () => {
    expect(parseBlocks("one\ntwo\n\nthree")).toEqual([
      { kind: "p", text: "one\ntwo" },
      { kind: "p", text: "three" },
    ]);
  });

  it("captures fenced code verbatim, fence lines excluded", () => {
    const blocks = parseBlocks("before\n```sh\nmake up\n**not bold in here**\n```");
    expect(blocks).toEqual([
      { kind: "p", text: "before" },
      { kind: "code", lang: "sh", text: "make up\n**not bold in here**" },
    ]);
  });

  it("an unterminated fence still becomes a code block", () => {
    const blocks = parseBlocks("```\ntruncated stream");
    expect(blocks).toEqual([{ kind: "code", lang: "", text: "truncated stream" }]);
  });

  it("headings carry their level", () => {
    expect(parseBlocks("### Deep")).toEqual([{ kind: "heading", level: 3, text: "Deep" }]);
  });

  it("bullet and numbered lists group their items", () => {
    expect(parseBlocks("- a\n- b\n\n1. x\n2) y")).toEqual([
      { kind: "list", ordered: false, items: ["a", "b"] },
      { kind: "list", ordered: true, items: ["x", "y"] },
    ]);
  });

  it("quotes join their lines", () => {
    expect(parseBlocks("> first\n> second")).toEqual([
      { kind: "quote", text: "first\nsecond" },
    ]);
  });

  it("horizontal rules are their own block, not a paragraph of dashes", () => {
    expect(parseBlocks("above\n\n---\n\nbelow")).toEqual([
      { kind: "p", text: "above" },
      { kind: "hr" },
      { kind: "p", text: "below" },
    ]);
  });

  it("plain text passes through untouched", () => {
    expect(parseBlocks("no markdown at all")).toEqual([
      { kind: "p", text: "no markdown at all" },
    ]);
  });
});
