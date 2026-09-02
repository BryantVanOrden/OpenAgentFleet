import { Fragment, type ReactNode } from "react";

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
  | { kind: "list"; ordered: boolean; items: string[] }
  | { kind: "quote"; text: string }
  | { kind: "hr" };

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
      const items: string[] = [(bullet ?? numbered)![1]];
      while (i + 1 < lines.length) {
        const nt = lines[i + 1].trim();
        const nb = ordered ? /^\d{1,3}[.)]\s+(.*)$/.exec(nt) : /^[-*+]\s+(.*)$/.exec(nt);
        if (!nb) break;
        items.push(nb[1]);
        i++;
      }
      blocks.push({ kind: "list", ordered, items });
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

/** Inline tokens for one run of text: code spans, bold, italic, strike, links. */
export function renderInline(text: string, keyBase = "i"): ReactNode[] {
  const out: ReactNode[] = [];
  // One combined scanner so constructs cannot half-overlap: earliest match wins.
  const re =
    /(`[^`]+`)|(\*\*[^*]+\*\*)|(__[^_]+__)|(\*[^*\s][^*]*\*)|(~~[^~]+~~)|(!?\[[^\]]*\]\([^)]*\))/g;
  let last = 0;
  let k = 0;
  for (let m = re.exec(text); m; m = re.exec(text)) {
    if (m.index > last) out.push(text.slice(last, m.index));
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
    } else if (tok.startsWith("*")) {
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
  if (last < text.length) out.push(text.slice(last));
  return out;
}

function linesOf(text: string, keyBase: string): ReactNode[] {
  return text.split("\n").map((l, i) => (
    <Fragment key={`${keyBase}-l${i}`}>
      {i > 0 && <br />}
      {renderInline(l, `${keyBase}-l${i}`)}
    </Fragment>
  ));
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
            return b.ordered ? (
              <ol key={key} className="my-1 list-decimal space-y-0.5 pl-5">
                {b.items.map((it, j) => (
                  <li key={`${key}-${j}`}>{renderInline(it, `${key}-${j}`)}</li>
                ))}
              </ol>
            ) : (
              <ul key={key} className="my-1 list-disc space-y-0.5 pl-5">
                {b.items.map((it, j) => (
                  <li key={`${key}-${j}`}>{renderInline(it, `${key}-${j}`)}</li>
                ))}
              </ul>
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
