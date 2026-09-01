import { useEffect, useMemo, useRef, useState } from "react";
import type { WorkItem } from "../lib/api";
import { Button } from "./ui";

/**
 * Runs a mini-app an agent published.
 *
 * The document goes into an iframe via srcdoc with sandbox="allow-scripts"
 * only — no allow-same-origin, so the page runs on an opaque origin with no
 * cookies, no access to the console's token, and nothing to reach even if it
 * tried. Anything an agent wants its app to have must be inside the document.
 */

/**
 * Prepends a Content-Security-Policy that denies the page any network.
 *
 * Inserted rather than required of the author: an agent writing a game should
 * not have to remember a security header, and one that forgot would otherwise
 * be trusted. The sandbox attribute alone does not stop fetch, XMLHttpRequest,
 * a WebSocket, or an <img> src assigned at runtime — the CSP is what holds.
 * Put first inside <head> so it applies before anything in the document acts.
 */
function sandboxed(html: string): string {
  const csp =
    '<meta http-equiv="Content-Security-Policy" ' +
    "content=\"default-src 'none'; " +
    "img-src data: blob:; media-src data: blob:; " +
    "style-src 'unsafe-inline'; " +
    "script-src 'unsafe-inline' 'unsafe-eval'; " +
    "font-src data:; " +
    "connect-src 'none'; form-action 'none'; base-uri 'none'\">";

  // Reports the first script error out to the console shell. Registered before
  // anything else in the document runs, so a parse error in the page's own
  // script is still caught. postMessage is the only channel out; the payload
  // is displayed as plain text and never executed or parsed.
  const reporter =
    '<script>window.addEventListener("error",function(e){' +
    'try{parent.postMessage({agentfleetMiniAppError:String(e.message||"script error")},"*");}' +
    "catch(_){}});</script>";

  const head = /<head[^>]*>/i.exec(html);
  if (head) {
    const at = head.index + head[0].length;
    return html.slice(0, at) + csp + reporter + html.slice(at);
  }
  // No head element: the browser will make one, so put the policy at the top
  // where it still lands inside it.
  return csp + reporter + html;
}

export default function MiniAppPlayer({
  item,
  onClose,
}: {
  item: WorkItem;
  onClose: () => void;
}) {
  /** Bumped on Restart so the iframe remounts with a fresh document. */
  const [generation, setGeneration] = useState(0);

  /**
   * The first script error the page reported, if any. An app whose JavaScript
   * will not parse otherwise shows a blank screen — indistinguishable from a
   * game that draws nothing yet — so the failure is surfaced instead of hidden.
   */
  const [scriptError, setScriptError] = useState<string | null>(null);
  const frameRef = useRef<HTMLIFrameElement>(null);

  const doc = useMemo(() => sandboxed(item.content ?? ""), [item.content]);

  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      if (e.source !== frameRef.current?.contentWindow) return;
      const err = (e.data as { agentfleetMiniAppError?: unknown } | null)?.agentfleetMiniAppError;
      if (typeof err === "string") setScriptError((prev) => prev ?? err);
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div className="fixed inset-0 z-50 flex flex-col bg-black">
      <header className="flex items-center gap-3 border-b border-ink-800 bg-ink-900 px-4 py-3">
        <h2 className="min-w-0 flex-1 truncate text-sm font-semibold text-ink-100">{item.name}</h2>
        <Button
          size="sm"
          onClick={() => {
            setScriptError(null);
            setGeneration((g) => g + 1);
          }}
        >
          Restart
        </Button>
        <Button size="sm" variant="ghost" onClick={onClose} aria-label="Close">
          ✕
        </Button>
      </header>

      <div className="relative min-h-0 flex-1">
        <iframe
          key={generation}
          ref={frameRef}
          title={item.name}
          srcDoc={doc}
          sandbox="allow-scripts"
          className="size-full border-0 bg-black"
        />
        {scriptError && (
          <div className="absolute inset-x-0 bottom-0 bg-bad-500/90 px-4 py-3 text-xs leading-relaxed text-white">
            This app has a script error, so it will not run properly:
            <br />
            {scriptError}
          </div>
        )}
      </div>
    </div>
  );
}
