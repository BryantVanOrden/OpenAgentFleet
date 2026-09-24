import { useEffect, useRef, useState, type ReactNode } from "react";
import { useLocation } from "react-router-dom";
import xrpQr from "../assets/support/xrp_qr.png";
import btcQr from "../assets/support/btc_qr.png";
import { Button, Card } from "./ui";

// The project's own donation details. Kept in one place and never derived: a
// single wrong character here sends someone's money into the void. They match
// the README and the public site, and the XRP QR already carries the tag.
const XRP_ADDRESS = "rf82s1CDagppvM6ATqc1nSrL6GackzHJrm";
const XRP_TAG = "796343731";
const BTC_ADDRESS = "bc1qvre807vxh08puxwc2z5adnm59tta7v5mqmky45";

/** The anchor the sidebar's Support link points at (/settings#support). */
export const SUPPORT_ANCHOR = "support";

/**
 * Where the project's donation addresses live.
 *
 * Deliberately passive: it sits at the bottom of Settings and is reached from a
 * quiet link in the sidebar footer. Nothing pops up, nothing counts visits.
 */
export default function SupportCard() {
  const ref = useScrollIntoViewOnHash(SUPPORT_ANCHOR);

  return (
    <div id={SUPPORT_ANCHOR} ref={ref} className="scroll-mt-6">
      <Card title="Support this project">
        <p className="mb-4 text-xs text-ink-400">
          OpenAgentFleet is free and open source. If it is useful to you, you can support
          development with XRP or Bitcoin.
        </p>

        <div className="grid gap-4 md:grid-cols-2">
          <Coin
            name="XRP"
            network="Ripple"
            qr={xrpQr}
            qrAlt={`QR code for the XRP address ${XRP_ADDRESS} with destination tag ${XRP_TAG}`}
          >
            <CopyRow label="Address" value={XRP_ADDRESS} copyLabel="Copy the XRP address" />
            <CopyRow
              label="Destination tag / memo (required)"
              value={XRP_TAG}
              copyLabel="Copy the XRP destination tag"
            />
            <p
              role="note"
              className="rounded-lg bg-warn-500/10 px-3 py-2 text-xs text-warn-500 ring-1 ring-inset ring-warn-500/30"
            >
              <strong className="font-semibold">
                A destination tag is required when sending XRP to this address ({XRP_TAG}).
              </strong>{" "}
              Without it the transfer will not be credited.
            </p>
          </Coin>

          <Coin
            name="Bitcoin"
            network="BTC"
            qr={btcQr}
            qrAlt={`QR code for the Bitcoin address ${BTC_ADDRESS}`}
          >
            <CopyRow label="Address" value={BTC_ADDRESS} copyLabel="Copy the Bitcoin address" />
            <p className="text-xs text-ink-500">No memo required.</p>
          </Coin>
        </div>
      </Card>
    </div>
  );
}

function Coin({
  name,
  network,
  qr,
  qrAlt,
  children,
}: {
  name: string;
  network: string;
  qr: string;
  qrAlt: string;
  children: ReactNode;
}) {
  return (
    <section
      aria-label={`${name} (${network})`}
      className="flex min-w-0 flex-col gap-3 rounded-lg bg-ink-850 p-4 ring-1 ring-ink-700"
    >
      <h3 className="text-sm font-medium text-ink-100">
        {name} <span className="font-normal text-ink-400">({network})</span>
      </h3>
      {/* Always a white tile, whatever the theme: a QR code needs light
          modules around it to scan, and these images have almost no quiet
          zone of their own. */}
      <div className="self-center rounded-lg bg-white p-3">
        <img
          src={qr}
          alt={qrAlt}
          width={250}
          height={250}
          loading="lazy"
          className="block size-44 [image-rendering:pixelated]"
        />
      </div>
      {children}
    </section>
  );
}

function CopyRow({ label, value, copyLabel }: { label: string; value: string; copyLabel: string }) {
  const valueRef = useRef<HTMLElement>(null);
  const [status, setStatus] = useState<"idle" | "copied" | "failed">("idle");

  useEffect(() => {
    if (status === "idle") return;
    const t = setTimeout(() => setStatus("idle"), 2000);
    return () => clearTimeout(t);
  }, [status]);

  const copy = async () => {
    const ok = await copyText(value);
    if (!ok && valueRef.current) {
      // Nothing to copy with (a plain-http origin has no clipboard API): leave
      // the value selected so the keyboard shortcut finishes the job.
      const range = document.createRange();
      range.selectNodeContents(valueRef.current);
      const sel = window.getSelection();
      sel?.removeAllRanges();
      sel?.addRange(range);
    }
    setStatus(ok ? "copied" : "failed");
  };

  return (
    <div>
      <div className="mb-1 text-xs text-ink-400">{label}</div>
      <div className="flex items-start gap-2 rounded-lg bg-ink-950 py-2 pr-2 pl-3 ring-1 ring-ink-700">
        <code
          ref={valueRef}
          className="min-w-0 flex-1 self-center font-mono text-sm break-all text-ink-100 select-all"
        >
          {value}
        </code>
        <Button
          type="button"
          size="sm"
          className="shrink-0"
          onClick={() => void copy()}
          aria-label={copyLabel}
        >
          {status === "copied" ? (
            <>
              <span className="text-good-500" aria-hidden>
                ✓
              </span>
              Copied
            </>
          ) : status === "failed" ? (
            "Selected"
          ) : (
            "Copy"
          )}
        </Button>
        <span className="sr-only" aria-live="polite">
          {status === "copied" ? `${label} copied` : status === "failed" ? `${label} selected, press your copy shortcut` : ""}
        </span>
      </div>
    </div>
  );
}

async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // Permission refused: fall through to the legacy path.
  }
  try {
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand("copy");
    ta.remove();
    return ok;
  } catch {
    return false;
  }
}

/**
 * Bring the element into view when the URL's hash names it.
 *
 * Settings fills in asynchronously — every card above this one grows once its
 * data arrives — so a single scroll lands short. The element is re-aligned
 * while the page is still settling, and left alone the moment the person
 * scrolls, taps or types themselves.
 */
function useScrollIntoViewOnHash(anchor: string) {
  const ref = useRef<HTMLDivElement>(null);
  const location = useLocation();

  useEffect(() => {
    const el = ref.current;
    if (location.hash !== `#${anchor}` || !el) return;

    const align = () => el.scrollIntoView({ block: "start" });
    align();

    const observer = new ResizeObserver(align);
    const page = el.parentElement;
    if (page) observer.observe(page);

    const stop = () => {
      observer.disconnect();
      window.clearTimeout(timer);
      for (const e of USER_SCROLL_EVENTS) window.removeEventListener(e, stop, true);
    };
    const timer = window.setTimeout(stop, 3000);
    for (const e of USER_SCROLL_EVENTS) window.addEventListener(e, stop, { capture: true, passive: true });
    return stop;
    // location.key: clicking the link again while already here re-scrolls.
  }, [anchor, location.hash, location.key]);

  return ref;
}

const USER_SCROLL_EVENTS = ["wheel", "touchstart", "keydown", "mousedown"] as const;
