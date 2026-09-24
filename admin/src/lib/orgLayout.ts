/**
 * Tidy top-down tree layout for the org chart.
 *
 * Reingold–Tilford in spirit: every subtree is laid out on its own, then its
 * siblings are pushed apart only as far as their contours (the leftmost and
 * rightmost extent at each depth) require, and a parent sits centred over its
 * first and last child. The result is compact — a narrow subtree tucks in
 * under a wide neighbour's shoulder — and symmetric: a mirrored tree lays out
 * mirrored.
 *
 * Pure and DOM-free so it can be tested; the page only draws what this says.
 */

export interface TreeInput {
  id: string;
  /** The node this one hangs from. Missing, unknown or cyclic parents are
   *  treated as no parent, so a bad row degrades to a root, never a crash. */
  parentId?: string | null;
}

export interface LayoutOptions {
  nodeWidth: number;
  nodeHeight: number;
  /** Space between neighbouring boxes on one row. */
  hGap: number;
  /** Space between a row and the next. */
  vGap: number;
}

export interface PlacedNode {
  id: string;
  parentId: string | null;
  depth: number;
  /** Top-left corner, in layout units, with the whole tree starting at 0,0. */
  x: number;
  y: number;
  children: string[];
}

export interface TreeLayout {
  nodes: Record<string, PlacedNode>;
  /** Parent to child, in drawing order. */
  edges: { from: string; to: string }[];
  width: number;
  height: number;
  roots: string[];
}

/** For each node, the parent it will actually be drawn under. */
export function resolveParents(items: TreeInput[]): Map<string, string | null> {
  const ids = new Set(items.map((i) => i.id));
  const parent = new Map<string, string | null>();
  for (const i of items) {
    const p = i.parentId && i.parentId !== i.id && ids.has(i.parentId) ? i.parentId : null;
    parent.set(i.id, p);
  }
  // Break cycles: walk up from every node; a walk that comes back to where it
  // started cuts the loop at the starting node, which becomes a root.
  for (const i of items) {
    const seen = new Set<string>([i.id]);
    let cur = parent.get(i.id) ?? null;
    while (cur) {
      if (seen.has(cur)) {
        parent.set(i.id, null);
        break;
      }
      seen.add(cur);
      cur = parent.get(cur) ?? null;
    }
  }
  return parent;
}

interface Sub {
  /** Offsets relative to this subtree's root centre, per depth below it. */
  left: number[];
  right: number[];
  /** Child centre offsets relative to this root's centre. */
  childOffsets: number[];
}

export function layoutTree(items: TreeInput[], opts: LayoutOptions): TreeLayout {
  const parent = resolveParents(items);
  const order = new Map(items.map((i, n) => [i.id, n]));
  const children = new Map<string, string[]>();
  const roots: string[] = [];
  for (const i of items) {
    const p = parent.get(i.id) ?? null;
    if (p) {
      const list = children.get(p) ?? [];
      list.push(i.id);
      children.set(p, list);
    } else {
      roots.push(i.id);
    }
  }
  // Input order is the sibling order: the caller decides (by name, say).
  for (const list of children.values()) list.sort((a, b) => order.get(a)! - order.get(b)!);

  const half = opts.nodeWidth / 2;
  const subs = new Map<string, Sub>();

  // Post-order, iteratively: org charts are shallow, but a pathological
  // chain should not blow the stack either.
  const post: string[] = [];
  const stack = [...roots].reverse();
  while (stack.length) {
    const id = stack.pop()!;
    post.push(id);
    const kids = children.get(id) ?? [];
    for (let k = kids.length - 1; k >= 0; k--) stack.push(kids[k]);
  }
  post.reverse(); // children before parents

  for (const id of post) {
    const kids = children.get(id) ?? [];
    if (kids.length === 0) {
      subs.set(id, { left: [-half], right: [half], childOffsets: [] });
      continue;
    }
    // Place children left to right, each as close as its contour allows.
    const offsets: number[] = [];
    let accLeft: number[] = [];
    let accRight: number[] = [];
    kids.forEach((kid, idx) => {
      const s = subs.get(kid)!;
      let shift = 0;
      if (idx > 0) {
        const depth = Math.min(accRight.length, s.left.length);
        shift = -Infinity;
        for (let d = 0; d < depth; d++) {
          shift = Math.max(shift, accRight[d] - s.left[d] + opts.hGap);
        }
      }
      offsets.push(shift);
      for (let d = 0; d < s.left.length; d++) {
        const l = s.left[d] + shift;
        const r = s.right[d] + shift;
        accLeft[d] = d < accLeft.length ? Math.min(accLeft[d], l) : l;
        accRight[d] = d < accRight.length ? Math.max(accRight[d], r) : r;
      }
    });
    // Centre the parent over its first and last child.
    const centre = (offsets[0] + offsets[offsets.length - 1]) / 2;
    const childOffsets = offsets.map((o) => o - centre);
    accLeft = accLeft.map((v) => v - centre);
    accRight = accRight.map((v) => v - centre);
    subs.set(id, {
      left: [-half, ...accLeft],
      right: [half, ...accRight],
      childOffsets,
    });
  }

  // Forest: lay the roots side by side with the same contour rule.
  const rootOffsets: number[] = [];
  {
    let accRight: number[] = [];
    roots.forEach((r, idx) => {
      const s = subs.get(r)!;
      let shift = 0;
      if (idx > 0) {
        shift = -Infinity;
        const depth = Math.min(accRight.length, s.left.length);
        for (let d = 0; d < depth; d++) shift = Math.max(shift, accRight[d] - s.left[d] + opts.hGap);
      }
      rootOffsets.push(shift);
      for (let d = 0; d < s.right.length; d++) {
        const v = s.right[d] + shift;
        accRight[d] = d < accRight.length ? Math.max(accRight[d], v) : v;
      }
    });
  }

  // Pre-order: absolute centres.
  const centreX = new Map<string, number>();
  const depthOf = new Map<string, number>();
  roots.forEach((r, i) => {
    centreX.set(r, rootOffsets[i]);
    depthOf.set(r, 0);
  });
  for (let i = post.length - 1; i >= 0; i--) {
    const id = post[i];
    const kids = children.get(id) ?? [];
    const s = subs.get(id)!;
    kids.forEach((kid, k) => {
      centreX.set(kid, centreX.get(id)! + s.childOffsets[k]);
      depthOf.set(kid, depthOf.get(id)! + 1);
    });
  }

  let minX = Infinity;
  let maxX = -Infinity;
  let maxDepth = 0;
  for (const id of post) {
    minX = Math.min(minX, centreX.get(id)! - half);
    maxX = Math.max(maxX, centreX.get(id)! + half);
    maxDepth = Math.max(maxDepth, depthOf.get(id)!);
  }
  if (!Number.isFinite(minX)) {
    return { nodes: {}, edges: [], width: 0, height: 0, roots: [] };
  }

  const nodes: Record<string, PlacedNode> = {};
  const edges: { from: string; to: string }[] = [];
  for (const i of items) {
    const depth = depthOf.get(i.id)!;
    nodes[i.id] = {
      id: i.id,
      parentId: parent.get(i.id) ?? null,
      depth,
      x: centreX.get(i.id)! - half - minX,
      y: depth * (opts.nodeHeight + opts.vGap),
      children: children.get(i.id) ?? [],
    };
  }
  for (const id of [...post].reverse()) {
    for (const kid of children.get(id) ?? []) edges.push({ from: id, to: kid });
  }
  return {
    nodes,
    edges,
    roots,
    width: maxX - minX,
    height: (maxDepth + 1) * opts.nodeHeight + maxDepth * opts.vGap,
  };
}

/**
 * An elbow connector from the bottom centre of a parent box to the top centre
 * of a child box: down, across at the midpoint of the gap, down. Corners are
 * rounded by `radius`, clamped so a short jog never overshoots.
 */
export function elbowPath(
  from: { x: number; y: number },
  to: { x: number; y: number },
  radius = 10,
): string {
  const midY = (from.y + to.y) / 2;
  const dx = to.x - from.x;
  if (Math.abs(dx) < 0.5) return `M ${from.x} ${from.y} V ${to.y}`;
  const dir = dx > 0 ? 1 : -1;
  const r = Math.max(0, Math.min(radius, Math.abs(dx) / 2, (to.y - from.y) / 2));
  return [
    `M ${from.x} ${from.y}`,
    `V ${midY - r}`,
    `Q ${from.x} ${midY} ${from.x + dir * r} ${midY}`,
    `H ${to.x - dir * r}`,
    `Q ${to.x} ${midY} ${to.x} ${midY + r}`,
    `V ${to.y}`,
  ].join(" ");
}

/** Whether `id` is `ancestor` or sits somewhere under it. Used to show that a
 *  drop would make a reporting loop before the server says so. */
export function isInSubtree(
  parents: Map<string, string | null>,
  id: string,
  ancestor: string,
): boolean {
  let cur: string | null = id;
  const seen = new Set<string>();
  while (cur && !seen.has(cur)) {
    if (cur === ancestor) return true;
    seen.add(cur);
    cur = parents.get(cur) ?? null;
  }
  return false;
}

/** The scale and offset that fit a `w`×`h` layout inside a viewport with
 *  `pad` all round, never zooming in past `maxScale`. */
export function fitView(
  w: number,
  h: number,
  viewW: number,
  viewH: number,
  pad = 40,
  maxScale = 1,
  minScale = 0.2,
): { scale: number; x: number; y: number } {
  if (w <= 0 || h <= 0 || viewW <= 0 || viewH <= 0) return { scale: 1, x: pad, y: pad };
  const scale = Math.max(
    minScale,
    Math.min(maxScale, (viewW - pad * 2) / w, (viewH - pad * 2) / h),
  );
  return { scale, x: (viewW - w * scale) / 2, y: Math.max(pad, (viewH - h * scale) / 2) };
}
