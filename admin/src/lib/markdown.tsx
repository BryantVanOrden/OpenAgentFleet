import { Fragment, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { splitTicketRefs, workLink } from "./tickets";

/**
 * Renders the markdown agents actually write — emphasis, inline code, fenced
 * blocks, lists, headings, links, quotes — as React elements.
 *
 * Hand-rolled on purpose: everything is built as elements, so there is no
 * dangerouslySetInnerHTML for a hostile message to ride in on, and an
 * unmodelled construct degrades to its plain text instead of leaking syntax.
 * The block grammar lives in parseBlocks(), exported pure so it can be tested
 * without a DOM.
 */

export type Block =
  | { kind: "p"; text: string }
  | { kind: "heading"; level: number; text: string }
  | { kind: "code"; lang: string; text: string }
  /** `levels`, when present, is each item's nesting depth (0 is the top). */
  | { kind: "list"; ordered: boolean; items: string[]; levels?: number[]; start?: number }
  | { kind: "quote"; text: string }
  | { kind: "hr" };

/** Leading whitespace, with a tab as four spaces. */
function indentOf(line: string): number {
  let n = 0;
  for (const ch of line) {
    if (ch === " ") n++;
    else if (ch === "\t") n += 4;
    else break;
  }
  return n;
}

/** A flat list with depths, as a tree: each item and the items under it. */
export interface ListNode {
  text: string;
  children: ListNode[];
}

export function nestList(items: string[], levels?: number[]): ListNode[] {
  const root: ListNode[] = [];
  const path: ListNode[][] = [root];
  items.forEach((text, i) => {
    // An item can only be one level deeper than the one before it.
    const want = Math.min(levels?.[i] ?? 0, path.length - 1);
    path.length = want + 1;
    const node: ListNode = { text, children: [] };
    path[want].push(node);
    path.push(node.children);
  });
  return root;
}

export function parseBlocks(md: string): Block[] {
  const lines = md.replace(/\r\n/g, "\n").split("\n");
  const blocks: Block[] = [];
  let para: string[] = [];

  const flush = () => {
    const text = para.join("\n").trim();
    if (text) blocks.push({ kind: "p", text });
    para = [];
  };

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const t = line.trim();

    const fence = /^```(\S*)\s*$/.exec(t);
    if (fence) {
      flush();
      const body: string[] = [];
      i++;
      while (i < lines.length && !/^```\s*$/.test(lines[i].trim())) {
        body.push(lines[i]);
        i++;
      }
      blocks.push({ kind: "code", lang: fence[1], text: body.join("\n") });
      continue;
    }

    const heading = /^(#{1,6})\s+(.*)$/.exec(t);
    if (heading) {
      flush();
      blocks.push({ kind: "heading", level: heading[1].length, text: heading[2] });
      continue;
    }

    if (/^(-{3,}|\*{3,}|_{3,})$/.test(t)) {
      flush();
      blocks.push({ kind: "hr" });
      continue;
    }

    const bullet = /^[-*+]\s+(.*)$/.exec(t);
    const numbered = /^\d{1,3}[.)]\s+(.*)$/.exec(t);
    if (bullet || numbered) {
      flush();
      const ordered = Boolean(numbered);
      const base = indentOf(line);
      const items: string[] = [(bullet ?? numbered)![1]];
      const levels: number[] = [0];
      // Indent of each open nesting level, so "  - x" under "- y" is one
      // level deeper whether the writer indents by two spaces or four.
      const stops: number[] = [base];
      const start = numbered ? Number(/^(\d{1,3})/.exec(t)![1]) : 1;
      const sameKind = (s: string) => (ordered ? /^\d{1,3}[.)]\s+/.test(s) : /^[-*+]\s+/.test(s));
      while (i + 1 < lines.length) {
        // A blank line between items of the same list is a loose list, not
        // the end of it: "1." after a blank line is still item 2.
        if (lines[i + 1].trim() === "") {
          let j = i + 1;
          while (j < lines.length && lines[j].trim() === "") j++;
          if (j < lines.length && sameKind(lines[j].trim()) && indentOf(lines[j]) <= base + 1) {
            i = j - 1;
          } else {
            break;
          }
        }
        const raw = lines[i + 1];
        const nt = raw.trim();
        const ind = indentOf(raw);
        const any = /^(?:[-*+]|\d{1,3}[.)])\s+(.*)$/.exec(nt);
        const same = ordered ? /^\d{1,3}[.)]\s+(.*)$/.exec(nt) : /^[-*+]\s+(.*)$/.exec(nt);
        // A deeper item of either kind belongs to this list; at the top level
        // only the same kind does.
        const nb = ind > base ? any : same;
        if (!nb) break;
        while (stops.length > 1 && ind < stops[stops.length - 1]) stops.pop();
        if (ind > stops[stops.length - 1] && stops.length < 6) stops.push(ind);
        items.push(nb[1]);
        levels.push(stops.length - 1);
        i++;
      }
      const block: Block = { kind: "list", ordered, items };
      if (levels.some((l) => l > 0)) block.levels = levels;
      if (ordered && start !== 1) block.start = start;
      blocks.push(block);
      continue;
    }

    const quote = /^>\s?(.*)$/.exec(t);
    if (quote) {
      flush();
      const body: string[] = [quote[1]];
      while (i + 1 < lines.length) {
        const nq = /^>\s?(.*)$/.exec(lines[i + 1].trim());
        if (!nq) break;
        body.push(nq[1]);
        i++;
      }
      blocks.push({ kind: "quote", text: body.join("\n") });
      continue;
    }

    if (t === "") {
      flush();
      continue;
    }
    para.push(line);
  }
  flush();
  return blocks;
}

/**
 * Plain text with ticket references (T-12) turned into links to the board.
 * Only plain runs go through here — never code spans or link labels — so a
 * reference quoted in backticks stays literal.
 */
function withTicketLinks(text: string, keyBase: string): ReactNode[] {
  return splitTicketRefs(text).map((part, i) =>
    typeof part === "string" ? (
      part
    ) : (
      <Link
        key={`${keyBase}-t${i}`}
        to={workLink(part.ref)}
        className="rounded font-mono text-[0.95em] text-live-500 underline decoration-live-500/40 underline-offset-2 hover:decoration-live-500"
        title={`Open ${part.ref} on the Work board`}
      >
        {part.ref}
      </Link>
    ),
  );
}

/** Inline tokens for one run of text: code spans, bold, italic, strike, links. */
export function renderInline(text: string, keyBase = "i"): ReactNode[] {
  const out: ReactNode[] = [];
  // One combined scanner so constructs cannot half-overlap: earliest match wins.
  const re =
    /(`[^`]+`)|(\*\*[^*]+\*\*)|(__[^_]+__)|(\*[^*\s][^*]*\*)|((?<!\w)_[^_\s](?:[^_\n]*[^_\s])?_(?!\w))|(~~[^~]+~~)|(!?\[[^\]]*\]\([^)]*\))/g;
  let last = 0;
  let k = 0;
  for (let m = re.exec(text); m; m = re.exec(text)) {
    if (m.index > last) out.push(...withTicketLinks(text.slice(last, m.index), `${keyBase}-p${k}`));
    const tok = m[0];
    const key = `${keyBase}-${k++}`;
    if (tok.startsWith("`")) {
      out.push(
        <code key={key} className="rounded bg-ink-950 px-1 py-0.5 font-mono text-[0.92em]">
          {tok.slice(1, -1)}
        </code>,
      );
    } else if (tok.startsWith("**") || tok.startsWith("__")) {
      out.push(<strong key={key}>{renderInline(tok.slice(2, -2), key)}</strong>);
    } else if (tok.startsWith("~~")) {
      out.push(<del key={key}>{renderInline(tok.slice(2, -2), key)}</del>);
    } else if (tok.startsWith("*") || tok.startsWith("_")) {
      // _italic_ as well as *italic*: the fleet's own replies (/tickets,
      // /org) use underscores. Only at word edges, so read_work stays a word.
      out.push(<em key={key}>{renderInline(tok.slice(1, -1), key)}</em>);
    } else {
      // [label](url) — or an image, which renders as its alt text; inlining
      // arbitrary remote images into an operator console is an exfil channel
      // (a hostile page the agent read can smuggle text into a URL it makes
      // the operator's browser fetch).
      const link = /^(!?)\[([^\]]*)\]\(([^)\s]*)[^)]*\)$/.exec(tok)!;
      const [, bang, label, href] = link;
      if (bang || !/^https?:\/\//i.test(href)) {
        out.push(label);
      } else {
        out.push(
          <a
            key={key}
            href={href}
            target="_blank"
            rel="noreferrer noopener"
            className="text-live-500 underline decoration-live-500/50 hover:decoration-live-500"
          >
            {label || href}
          </a>,
        );
      }
    }
    last = m.index + tok.length;
  }
  if (last < text.length) out.push(...withTicketLinks(text.slice(last), `${keyBase}-end`));
  return out;
}

/**
 * Only `code spans`, nothing else: for one-line labels such as a ticket's
 * title, which agents write with backticks, shown inside things that are
 * themselves clickable (so no links).
 */
export function renderCodeSpans(text: string): ReactNode[] {
  return text.split(/(`[^`]+`)/g).map((part, i) =>
    part.length > 2 && part.startsWith("`") && part.endsWith("`") ? (
      <code key={i} className="rounded bg-ink-950/70 px-1 py-px font-mono text-[0.9em]">
        {part.slice(1, -1)}
      </code>
    ) : (
      part
    ),
  );
}

function linesOf(text: string, keyBase: string): ReactNode[] {
  return text.split("\n").map((l, i) => (
    <Fragment key={`${keyBase}-l${i}`}>
      {i > 0 && <br />}
      {renderInline(l, `${keyBase}-l${i}`)}
    </Fragment>
  ));
}

function ListView({
  nodes,
  ordered,
  start,
  depth,
  keyBase,
}: {
  nodes: ListNode[];
  ordered: boolean;
  start?: number;
  depth: number;
  keyBase: string;
}) {
  // Nested levels get a hollow and then a square marker, and a faint guide
  // line, so a chart three levels deep still reads as a tree.
  const marker = ordered ? "list-decimal" : depth === 0 ? "list-disc" : depth === 1 ? "list-[circle]" : "list-[square]";
  const cls = `${marker} space-y-0.5 pl-5 ${depth === 0 ? "my-1" : "mt-0.5 border-l border-ink-700/70 ml-0.5"}`;
  const items = nodes.map((n, j) => (
    <li key={`${keyBase}-${j}`}>
      {renderInline(n.text, `${keyBase}-${j}`)}
      {n.children.length > 0 && (
        <ListView nodes={n.children} ordered={ordered} depth={depth + 1} keyBase={`${keyBase}-${j}`} />
      )}
    </li>
  ));
  return ordered ? (
    <ol className={cls} start={start}>
      {items}
    </ol>
  ) : (
    <ul className={cls}>{items}</ul>
  );
}

export function Markdown({ text, className }: { text: string; className?: string }) {
  const blocks = parseBlocks(text);
  return (
    <div className={className}>
      {blocks.map((b, i) => {
        const key = `b${i}`;
        switch (b.kind) {
          case "heading": {
            const cls = b.level <= 2 ? "text-[1.15em]" : "text-[1.05em]";
            return (
              <p key={key} className={`${cls} mt-1 mb-1 font-semibold`}>
                {renderInline(b.text, key)}
              </p>
            );
          }
          case "code":
            return (
              <pre
                key={key}
                className="my-1.5 overflow-x-auto rounded-lg border border-ink-700 bg-ink-950 p-2.5 font-mono text-[0.92em] whitespace-pre"
              >
                {b.text}
              </pre>
            );
          case "list":
            return (
              <ListView
                key={key}
                nodes={nestList(b.items, b.levels)}
                ordered={b.ordered}
                start={b.start}
                depth={0}
                keyBase={key}
              />
            );
          case "quote":
            return (
              <blockquote key={key} className="my-1 border-l-2 border-ink-600 pl-2.5 text-ink-300">
                {linesOf(b.text, key)}
              </blockquote>
            );
          case "hr":
            return <hr key={key} className="my-2 border-ink-700" />;
          default:
            return (
              <p key={key} className="my-0.5">
                {linesOf(b.text, key)}
              </p>
            );
        }
      })}
    </div>
  );
}
