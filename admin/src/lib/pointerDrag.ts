import { useCallback, useEffect, useRef, useState } from "react";

/**
 * Drag and drop on pointer events, so it works with a mouse, a pen and a
 * finger alike. HTML5 drag-and-drop never fires on touch screens.
 *
 * A mouse or pen drag starts once the pointer has moved a few pixels, so a
 * click stays a click. A finger has to hold still for a moment first (a long
 * press): a touch that moves straight away is a scroll or a pan, and is left
 * to whatever scrolls or pans.
 *
 * Drop targets are found under the pointer by a `data-drop="<id>"`
 * attribute, so any element can be one without registering. The item being
 * dragged is followed by a ghost the page draws; its position is written to
 * the element behind `ghostRef` directly, not through React state, so a drag
 * does not re-render the page sixty times a second.
 */

export type PointerKind = "mouse" | "pen" | "touch" | string;

/** How far a pointer may move before a press means something else. */
export const DRAG_SLOP = { mouse: 5, touch: 10 };
export const LONG_PRESS_MS = 350;

/**
 * What a press has become, from how far it moved and for how long. Pure, so
 * the thresholds can be tested: a mouse drags on moving; a finger drags on
 * holding still, and gives the gesture up (to scrolling) on moving first.
 */
export function pressIntent(kind: PointerKind, moved: number, heldMs: number): "wait" | "drag" | "release" {
  if (kind === "touch") {
    if (moved > DRAG_SLOP.touch) return "release";
    return heldMs >= LONG_PRESS_MS ? "drag" : "wait";
  }
  return moved > DRAG_SLOP.mouse ? "drag" : "wait";
}

/** Speed (px per frame) to scroll when the pointer is `inset` px inside an
 *  edge band `band` wide: nothing outside the band, faster nearer the edge. */
export function edgeSpeed(inset: number, band = 56, max = 14): number {
  if (inset >= band) return 0;
  const k = 1 - Math.max(0, inset) / band;
  return Math.round(max * k * k * 10) / 10;
}

export interface DragState {
  id: string;
  /** The drop target under the pointer, or null. */
  over: string | null;
  kind: PointerKind;
}

export interface PointerDragOptions {
  enabled: boolean;
  onDrop: (id: string, target: string) => void;
  onStart?: (id: string) => void;
  /** Scroll or pan when the pointer nears the edges of this element. */
  edge?: { el: () => HTMLElement | null; scroll: (dx: number, dy: number) => void };
}

export function usePointerDrag({ enabled, onDrop, onStart, edge }: PointerDragOptions) {
  const [drag, setDrag] = useState<DragState | null>(null);
  const ghostRef = useRef<HTMLDivElement | null>(null);

  // Latest callbacks, so the window listeners never go stale.
  const cb = useRef({ onDrop, onStart, edge });
  cb.current = { onDrop, onStart, edge };

  const st = useRef<{
    id: string;
    pointerId: number;
    kind: PointerKind;
    x0: number;
    y0: number;
    x: number;
    y: number;
    t0: number;
    dragging: boolean;
    over: string | null;
    timer?: number;
    raf?: number;
  } | null>(null);
  const cleanupRef = useRef<() => void>(() => undefined);

  // Beside a mouse pointer; above a finger, which would hide it; always on
  // screen, so a card carried to the edge is still seen.
  const placeGhost = (x: number, y: number) => {
    const g = ghostRef.current;
    if (!g) return;
    const w = g.offsetWidth;
    const h = g.offsetHeight;
    const touch = st.current?.kind === "touch";
    let gx = touch ? x - w / 2 : x + 14;
    let gy = touch ? y - h - 28 : y + 12;
    gx = Math.max(8, Math.min(gx, window.innerWidth - w - 8));
    gy = Math.max(8, Math.min(gy, window.innerHeight - h - 8));
    g.style.transform = `translate(${Math.round(gx)}px, ${Math.round(gy)}px)`;
  };

  const hitTest = (x: number, y: number): string | null => {
    const el = document.elementFromPoint(x, y) as HTMLElement | null;
    return el?.closest<HTMLElement>("[data-drop]")?.dataset.drop ?? null;
  };

  const updateOver = () => {
    const s = st.current;
    if (!s?.dragging) return;
    const over = hitTest(s.x, s.y);
    if (over !== s.over) {
      s.over = over;
      setDrag((d) => (d ? { ...d, over } : d));
    }
  };

  const end = useCallback((drop: boolean) => {
    const s = st.current;
    cleanupRef.current();
    st.current = null;
    if (!s) return;
    if (s.dragging) {
      setDrag(null);
      // The click that follows a drag belongs to the drag, not to whatever
      // the pointer happened to be released over.
      const swallow = (e: MouseEvent) => {
        e.preventDefault();
        e.stopPropagation();
      };
      window.addEventListener("click", swallow, { capture: true, once: true });
      window.setTimeout(() => window.removeEventListener("click", swallow, { capture: true }), 0);
      if (drop && s.over && s.over !== s.id) cb.current.onDrop(s.id, s.over);
    }
  }, []);

  const begin = () => {
    const s = st.current;
    if (!s || s.dragging) return;
    s.dragging = true;
    window.clearTimeout(s.timer);
    document.body.style.userSelect = "none";
    if (s.kind === "touch") navigator.vibrate?.(8);
    setDrag({ id: s.id, over: null, kind: s.kind });
    cb.current.onStart?.(s.id);
    // The ghost mounts on the next render; place it once it has.
    requestAnimationFrame(() => {
      placeGhost(s.x, s.y);
      updateOver();
    });
    const tick = () => {
      const cur = st.current;
      if (!cur?.dragging) return;
      const e = cb.current.edge;
      const el = e?.el();
      if (e && el) {
        const r = el.getBoundingClientRect();
        const dx = edgeSpeed(r.right - cur.x) - edgeSpeed(cur.x - r.left);
        const dy = edgeSpeed(r.bottom - cur.y) - edgeSpeed(cur.y - r.top);
        if (dx || dy) {
          e.scroll(dx, dy);
          updateOver();
        }
      }
      cur.raf = requestAnimationFrame(tick);
    };
    s.raf = requestAnimationFrame(tick);
  };

  const onPointerDown = (id: string) => (e: React.PointerEvent) => {
    if (!enabled || st.current) return;
    if (e.pointerType === "mouse" && e.button !== 0) return;
    if ((e.target as HTMLElement).closest("a,button,input,select,textarea,[data-no-drag]")) return;

    const s = {
      id,
      pointerId: e.pointerId,
      kind: e.pointerType,
      x0: e.clientX,
      y0: e.clientY,
      x: e.clientX,
      y: e.clientY,
      t0: performance.now(),
      dragging: false,
      over: null as string | null,
      timer: undefined as number | undefined,
      raf: undefined as number | undefined,
    };
    st.current = s;

    const move = (ev: PointerEvent) => {
      const cur = st.current;
      if (!cur || ev.pointerId !== cur.pointerId) return;
      cur.x = ev.clientX;
      cur.y = ev.clientY;
      if (!cur.dragging) {
        const intent = pressIntent(cur.kind, Math.hypot(cur.x - cur.x0, cur.y - cur.y0), performance.now() - cur.t0);
        if (intent === "drag") begin();
        else if (intent === "release") end(false);
        return;
      }
      ev.preventDefault();
      placeGhost(cur.x, cur.y);
      updateOver();
    };
    const up = (ev: PointerEvent) => {
      if (st.current && ev.pointerId === st.current.pointerId) end(true);
    };
    const cancel = (ev: PointerEvent) => {
      if (st.current && ev.pointerId === st.current.pointerId) end(false);
    };
    // A second finger is a pinch, not a drag.
    const otherDown = (ev: PointerEvent) => {
      if (st.current && ev.pointerId !== st.current.pointerId) end(false);
    };
    const key = (ev: KeyboardEvent) => {
      if (ev.key === "Escape" && st.current?.dragging) {
        ev.preventDefault();
        end(false);
      }
    };
    // Once a finger drag has begun the page must not scroll under it, and a
    // long press must not open the browser's context menu.
    const touchMove = (ev: TouchEvent) => {
      if (st.current?.dragging) ev.preventDefault();
    };
    const menu = (ev: Event) => ev.preventDefault();

    window.addEventListener("pointermove", move, { passive: false });
    window.addEventListener("pointerup", up);
    window.addEventListener("pointercancel", cancel);
    window.addEventListener("pointerdown", otherDown, true);
    window.addEventListener("keydown", key);
    window.addEventListener("touchmove", touchMove, { passive: false });
    window.addEventListener("contextmenu", menu);
    if (s.kind === "touch") {
      s.timer = window.setTimeout(() => {
        const cur = st.current;
        if (cur && !cur.dragging && pressIntent(cur.kind, Math.hypot(cur.x - cur.x0, cur.y - cur.y0), LONG_PRESS_MS) === "drag") {
          begin();
        }
      }, LONG_PRESS_MS);
    }
    cleanupRef.current = () => {
      window.clearTimeout(s.timer);
      if (s.raf) cancelAnimationFrame(s.raf);
      document.body.style.userSelect = "";
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      window.removeEventListener("pointercancel", cancel);
      window.removeEventListener("pointerdown", otherDown, true);
      window.removeEventListener("keydown", key);
      window.removeEventListener("touchmove", touchMove);
      window.removeEventListener("contextmenu", menu);
    };
  };

  // Leaving the page mid-drag must not leave listeners behind.
  useEffect(() => () => cleanupRef.current(), []);

  return {
    drag,
    ghostRef,
    /** Spread onto each draggable element. */
    bind: (id: string) => ({ onPointerDown: onPointerDown(id) }),
    cancel: () => end(false),
  };
}
