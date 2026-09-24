import { describe, expect, it } from "vitest";
import { elbowPath, fitView, isInSubtree, layoutTree, resolveParents } from "./orgLayout";

const opts = { nodeWidth: 100, nodeHeight: 50, hGap: 20, vGap: 40 };

const centre = (l: ReturnType<typeof layoutTree>, id: string) => l.nodes[id].x + opts.nodeWidth / 2;

/** No two boxes on one row may overlap or sit closer than the gap. */
function assertNoOverlap(l: ReturnType<typeof layoutTree>) {
  const rows = new Map<number, number[]>();
  for (const n of Object.values(l.nodes)) {
    const xs = rows.get(n.depth) ?? [];
    xs.push(n.x);
    rows.set(n.depth, xs);
  }
  for (const xs of rows.values()) {
    xs.sort((a, b) => a - b);
    for (let i = 1; i < xs.length; i++) {
      expect(xs[i] - xs[i - 1]).toBeGreaterThanOrEqual(opts.nodeWidth + opts.hGap - 1e-9);
    }
  }
}

describe("layoutTree", () => {
  it("lays out a single node at the origin", () => {
    const l = layoutTree([{ id: "you" }], opts);
    expect(l.nodes.you).toMatchObject({ x: 0, y: 0, depth: 0 });
    expect(l.width).toBe(100);
    expect(l.height).toBe(50);
    expect(l.edges).toEqual([]);
  });

  it("centres a parent over its children and puts children one row down", () => {
    const l = layoutTree(
      [{ id: "you" }, { id: "a", parentId: "you" }, { id: "b", parentId: "you" }, { id: "c", parentId: "you" }],
      opts,
    );
    expect(centre(l, "you")).toBeCloseTo((centre(l, "a") + centre(l, "c")) / 2);
    expect(centre(l, "b")).toBeCloseTo(centre(l, "you"));
    for (const id of ["a", "b", "c"]) expect(l.nodes[id].y).toBe(90);
    expect(l.nodes.a.x).toBe(0);
    expect(l.width).toBe(3 * 100 + 2 * 20);
    assertNoOverlap(l);
  });

  it("keeps the input order as sibling order", () => {
    const l = layoutTree([{ id: "r" }, { id: "z", parentId: "r" }, { id: "a", parentId: "r" }], opts);
    expect(l.nodes.z.x).toBeLessThan(l.nodes.a.x);
    expect(l.nodes.r.children).toEqual(["z", "a"]);
  });

  it("tucks a small subtree under a wide neighbour instead of spreading the row", () => {
    // r has two children; the left one has three reports, the right none.
    // The right child sits beside its sibling, not beyond the grandchildren.
    const l = layoutTree(
      [
        { id: "r" },
        { id: "a", parentId: "r" },
        { id: "b", parentId: "r" },
        { id: "a1", parentId: "a" },
        { id: "a2", parentId: "a" },
        { id: "a3", parentId: "a" },
      ],
      opts,
    );
    assertNoOverlap(l);
    expect(l.nodes.b.x - l.nodes.a.x).toBe(opts.nodeWidth + opts.hGap);
    expect(l.width).toBe(3 * 100 + 2 * 20);
  });

  it("is symmetric: a mirrored tree lays out mirrored", () => {
    const l = layoutTree(
      [
        { id: "r" },
        { id: "a", parentId: "r" },
        { id: "b", parentId: "r" },
        { id: "a1", parentId: "a" },
        { id: "a2", parentId: "a" },
        { id: "b1", parentId: "b" },
        { id: "b2", parentId: "b" },
      ],
      opts,
    );
    const mid = centre(l, "r");
    expect(mid - centre(l, "a")).toBeCloseTo(centre(l, "b") - mid);
    expect(mid - centre(l, "a1")).toBeCloseTo(centre(l, "b2") - mid);
    assertNoOverlap(l);
  });

  it("never overlaps on a bushy, uneven tree", () => {
    const items = [{ id: "r" }];
    let n = 0;
    const add = (parent: string, kids: number, depth: number) => {
      for (let i = 0; i < kids; i++) {
        const id = `n${n++}`;
        items.push({ id, parentId: parent } as { id: string });
        if (depth < 3) add(id, (i * 7 + depth) % 4, depth + 1);
      }
    };
    add("r", 5, 0);
    const l = layoutTree(items, opts);
    assertNoOverlap(l);
    expect(Object.keys(l.nodes)).toHaveLength(items.length);
    expect(Math.min(...Object.values(l.nodes).map((p) => p.x))).toBe(0);
    for (const e of l.edges) expect(l.nodes[e.to].depth).toBe(l.nodes[e.from].depth + 1);
  });

  it("treats unknown and self parents as roots and lays a forest side by side", () => {
    const l = layoutTree([{ id: "a", parentId: "ghost" }, { id: "b", parentId: "b" }], opts);
    expect(l.roots).toEqual(["a", "b"]);
    expect(l.nodes.a.depth).toBe(0);
    expect(l.nodes.b.depth).toBe(0);
    assertNoOverlap(l);
  });

  it("breaks a reporting loop rather than looping forever", () => {
    const l = layoutTree(
      [{ id: "you" }, { id: "a", parentId: "b" }, { id: "b", parentId: "a" }],
      opts,
    );
    expect(Object.keys(l.nodes).sort()).toEqual(["a", "b", "you"]);
    // Exactly one of the pair became a root; the other hangs from it.
    const depths = [l.nodes.a.depth, l.nodes.b.depth].sort();
    expect(depths).toEqual([0, 1]);
  });

  it("handles an empty input", () => {
    expect(layoutTree([], opts)).toEqual({ nodes: {}, edges: [], width: 0, height: 0, roots: [] });
  });
});

describe("resolveParents / isInSubtree", () => {
  const parents = resolveParents([
    { id: "a" },
    { id: "b", parentId: "a" },
    { id: "c", parentId: "b" },
    { id: "d", parentId: "a" },
  ]);

  it("finds descendants", () => {
    expect(isInSubtree(parents, "c", "a")).toBe(true);
    expect(isInSubtree(parents, "c", "b")).toBe(true);
    expect(isInSubtree(parents, "d", "b")).toBe(false);
    expect(isInSubtree(parents, "a", "a")).toBe(true);
  });
});

describe("elbowPath", () => {
  it("is a straight line when the child is directly below", () => {
    expect(elbowPath({ x: 50, y: 0 }, { x: 50, y: 100 })).toBe("M 50 0 V 100");
  });

  it("goes down, across at the midpoint, and down with rounded corners", () => {
    expect(elbowPath({ x: 0, y: 0 }, { x: 100, y: 100 }, 10)).toBe(
      "M 0 0 V 40 Q 0 50 10 50 H 90 Q 100 50 100 60 V 100",
    );
  });

  it("clamps the radius on a short jog", () => {
    const d = elbowPath({ x: 0, y: 0 }, { x: -6, y: 100 }, 10);
    expect(d).toBe("M 0 0 V 47 Q 0 50 -3 50 H -3 Q -6 50 -6 53 V 100");
  });
});

describe("fitView", () => {
  it("never zooms in past the maximum", () => {
    expect(fitView(100, 50, 1000, 800, 40, 1).scale).toBe(1);
  });

  it("scales a wide chart down and centres it", () => {
    const v = fitView(2000, 200, 1000, 800, 0, 1);
    expect(v.scale).toBe(0.5);
    expect(v.x).toBe(0);
    expect(v.y).toBe((800 - 100) / 2);
  });
});
