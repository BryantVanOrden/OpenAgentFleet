import { describe, expect, it } from "vitest";
import { nestList, parseBlocks, renderInline } from "./markdown";

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

  it("indented items nest, whatever the indent width (the /org reply)", () => {
    const md = "You\n- **Builder**\n  - **Claude** · _Claude Code_\n    - deep\n- **Checker**";
    const blocks = parseBlocks(md);
    expect(blocks).toEqual([
      { kind: "p", text: "You" },
      {
        kind: "list",
        ordered: false,
        items: ["**Builder**", "**Claude** · _Claude Code_", "deep", "**Checker**"],
        levels: [0, 1, 2, 0],
      },
    ]);
    const list = blocks[1] as { items: string[]; levels: number[] };
    expect(nestList(list.items, list.levels)).toEqual([
      {
        text: "**Builder**",
        children: [{ text: "**Claude** · _Claude Code_", children: [{ text: "deep", children: [] }] }],
      },
      { text: "**Checker**", children: [] },
    ]);
  });

  it("a nested numbered item inside a bullet list stays in the list", () => {
    expect(parseBlocks("- a\n    1. one\n- b")).toEqual([
      { kind: "list", ordered: false, items: ["a", "one", "b"], levels: [0, 1, 0] },
    ]);
  });

  it("a loose list (blank lines between items) is still one list", () => {
    expect(parseBlocks("1. one\n\n2. two\n\n3. three\n\nAfter.")).toEqual([
      { kind: "list", ordered: true, items: ["one", "two", "three"] },
      { kind: "p", text: "After." },
    ]);
  });

  it("a numbered list interrupted by a paragraph keeps counting from its own number", () => {
    expect(parseBlocks("1. one\n\nA note.\n\n2. two")).toEqual([
      { kind: "list", ordered: true, items: ["one"] },
      { kind: "p", text: "A note." },
      { kind: "list", ordered: true, items: ["two"], start: 2 },
    ]);
  });

  it("nestList never skips a level", () => {
    expect(nestList(["a", "b"], [0, 3])).toEqual([{ text: "a", children: [{ text: "b", children: [] }] }]);
  });

  it("_underscore_ italics at word edges, but not inside snake_case", () => {
    const shape = (s: string) =>
      renderInline(s).map((n) => (typeof n === "string" ? n : (n as { type: unknown }).type));
    expect(shape("T-3 · _in progress_ · Builder")).toContain("em");
    expect(shape("use read_work and publish_work")).not.toContain("em");
    expect(shape("a _b_c_ d")).not.toContain("em");
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
