import { describe, expect, it } from "vitest";
import type { TicketView } from "./api";
import {
  agentStatus,
  budgetFraction,
  cancelCascade,
  cascadeSummary,
  filterTickets,
  findWorkItem,
  groupByStatus,
  initials,
  money,
  openBlockerCount,
  parsePublished,
  reopenTargets,
  splitSharedFiles,
  splitTicketRefs,
  underAnyRoot,
} from "./tickets";

const t = (over: Partial<TicketView>): TicketView => ({
  id: over.id ?? "id",
  ref: `T-${over.number ?? 1}`,
  number: 1,
  title: "A ticket",
  kind: "work",
  status: "todo",
  attempts: 0,
  rounds: 0,
  wakes: 0,
  cost_usd: 0,
  blocked_by: [],
  created_at: "2026-09-01T00:00:00Z",
  updated_at: "2026-09-01T00:00:00Z",
  ...over,
});

describe("splitTicketRefs", () => {
  it("finds references at word boundaries", () => {
    expect(splitTicketRefs("see T-12, then T-3.")).toEqual(["see ", { ref: "T-12" }, ", then ", { ref: "T-3" }, "."]);
  });

  it("leaves lookalikes alone", () => {
    for (const s of ["AT-12", "T-12a", "t-12", "T-", "x/T-12", "T-12-b", "IT-4", "#T-5"]) {
      expect(splitTicketRefs(s)).toEqual([s]);
    }
  });

  it("matches in brackets and at the ends", () => {
    expect(splitTicketRefs("(T-7)")).toEqual(["(", { ref: "T-7" }, ")"]);
    expect(splitTicketRefs("T-1")).toEqual([{ ref: "T-1" }]);
  });

  it("returns plain text untouched", () => {
    expect(splitTicketRefs("nothing here")).toEqual(["nothing here"]);
    expect(splitTicketRefs("")).toEqual([]);
  });
});

describe("filterTickets", () => {
  const list = [
    t({ id: "a", number: 12, title: "Build the login page", assignee_id: "b1", assignee_name: "Builder" }),
    t({ id: "b", number: 13, title: "Review login", parent_id: "a", assignee_id: "c1", assignee_name: "Checker" }),
    t({ id: "c", number: 14, title: "Unowned" }),
  ];

  it("filters by assignee, including unassigned", () => {
    expect(filterTickets(list, { assignee: "b1" }).map((x) => x.id)).toEqual(["a"]);
    expect(filterTickets(list, { assignee: "none" }).map((x) => x.id)).toEqual(["c"]);
    expect(filterTickets(list, { assignee: "" })).toHaveLength(3);
  });

  it("keeps only top-level requests", () => {
    expect(filterTickets(list, { rootsOnly: true }).map((x) => x.id)).toEqual(["a", "c"]);
  });

  it("searches titles, names and references", () => {
    expect(filterTickets(list, { query: "login" }).map((x) => x.id)).toEqual(["a", "b"]);
    expect(filterTickets(list, { query: "checker" }).map((x) => x.id)).toEqual(["b"]);
    expect(filterTickets(list, { query: "T-14" }).map((x) => x.id)).toEqual(["c"]);
    expect(filterTickets(list, { query: "13" }).map((x) => x.id)).toEqual(["b"]);
  });
});

describe("groupByStatus", () => {
  it("puts every ticket in its column, newest first", () => {
    const g = groupByStatus([
      t({ id: "old", status: "todo", updated_at: "2026-09-01T00:00:00Z" }),
      t({ id: "new", status: "todo", updated_at: "2026-09-02T00:00:00Z" }),
      t({ id: "d", status: "done" }),
    ]);
    expect(g.todo.map((x) => x.id)).toEqual(["new", "old"]);
    expect(g.done.map((x) => x.id)).toEqual(["d"]);
    expect(g.cancelled).toEqual([]);
  });
});

describe("openBlockerCount", () => {
  it("counts open and unknown blockers, not finished ones", () => {
    const done = t({ id: "x", status: "done" });
    const open = t({ id: "y", status: "in_progress" });
    const byId = new Map([
      ["x", done],
      ["y", open],
    ]);
    expect(openBlockerCount(t({ blocked_by: ["x", "y", "gone"] }), byId)).toBe(2);
    expect(openBlockerCount(t({ blocked_by: null }), byId)).toBe(0);
  });
});

describe("agentStatus", () => {
  const base = { busy: false, online: true, state: "running", hold: "" };
  it("says what the agent is doing, most important first", () => {
    expect(agentStatus({ ...base, busy: true, ticket_ref: "T-12" })).toEqual({ tone: "live", label: "working on T-12" });
    expect(agentStatus(base)).toEqual({ tone: "good", label: "idle" });
    expect(agentStatus({ ...base, online: false })).toEqual({ tone: "idle", label: "offline" });
    expect(agentStatus({ ...base, online: false, state: "stopped" }).label).toBe("stopped");
    expect(agentStatus({ ...base, busy: true, hold: "budget" }).label).toBe("held at budget");
  });
});

describe("small formatters", () => {
  it("money", () => {
    expect(money(0)).toBe("$0");
    expect(money(0.004)).toBe("<$0.01");
    expect(money(3.1)).toBe("$3.10");
    expect(money(1234.4)).toBe("$1,234");
  });

  it("budgetFraction", () => {
    expect(budgetFraction(3, 0)).toBeNull();
    expect(budgetFraction(5, 10)).toBe(0.5);
    expect(budgetFraction(15, 10)).toBe(1);
  });

  it("initials", () => {
    expect(initials("Builder")).toBe("BU");
    expect(initials("gui-scout")).toBe("GS");
    expect(initials("")).toBe("?");
  });

  it("underAnyRoot compares like the server", () => {
    expect(underAnyRoot("C:\\work\\app", ["C:/Work"])).toBe(true);
    expect(underAnyRoot("C:/work", ["c:/work/"])).toBe(true);
    expect(underAnyRoot("C:/workshop", ["C:/work"])).toBe(false);
    expect(underAnyRoot("", ["C:/work"])).toBe(false);
  });
});

describe("cancelCascade", () => {
  // T-1 (request) > T-2 (done work), T-3 (open work) > T-4 (open, nested).
  // T-5 reviews T-3 and sits outside the tree, as review tickets do.
  // T-6 verifies T-1 (open); T-7 verifies T-1 but is done; T-8 is unrelated.
  const all = [
    t({ id: "1", number: 1, status: "in_progress" }),
    t({ id: "2", number: 2, parent_id: "1", status: "done" }),
    t({ id: "3", number: 3, parent_id: "1", status: "in_review" }),
    t({ id: "4", number: 4, parent_id: "3", status: "backlog" }),
    t({ id: "5", number: 5, kind: "review", target_id: "3", status: "todo" }),
    t({ id: "6", number: 6, kind: "verify", target_id: "1", status: "in_progress" }),
    t({ id: "7", number: 7, kind: "verify", target_id: "1", status: "done" }),
    t({ id: "8", number: 8, status: "todo" }),
  ];

  it("counts open parts at any depth and open checks of any of them", () => {
    const c = cancelCascade({ id: "1" }, all);
    expect(c.parts.map((x) => x.number)).toEqual([3, 4]);
    expect(c.checks.map((x) => x.number)).toEqual([5, 6]);
    expect(cascadeSummary(c)).toBe("2 open parts and 2 open checks");
  });

  it("a leaf with nothing open under it takes nothing with it", () => {
    const c = cancelCascade({ id: "8" }, all);
    expect(c.parts).toEqual([]);
    expect(c.checks).toEqual([]);
    expect(cascadeSummary(c)).toBe("");
  });

  it("survives a parent cycle in bad rows", () => {
    const loop = [t({ id: "a", parent_id: "b" }), t({ id: "b", parent_id: "a" })];
    expect(cancelCascade({ id: "a" }, loop).parts.map((x) => x.id)).toEqual(["b"]);
  });

  it("singular wording", () => {
    expect(cascadeSummary({ parts: [1], checks: [] })).toBe("1 open part");
    expect(cascadeSummary({ parts: [], checks: [1] })).toBe("1 open check");
  });
});

describe("reopenTargets", () => {
  const kids = [
    t({ id: "a", number: 3, status: "done" }),
    t({ id: "b", number: 2, status: "done", kind: "verify" }),
    t({ id: "c", number: 4, status: "in_progress" }),
    t({ id: "d", number: 1, status: "done" }),
  ];
  it("a request nobody is assigned reopens its finished work parts", () => {
    expect(reopenTargets({}, kids).map((x) => x.number)).toEqual([1, 3]);
  });
  it("an assigned ticket reopens itself", () => {
    expect(reopenTargets({ assignee_id: "x" }, kids)).toEqual([]);
    expect(reopenTargets({ assignee_user_id: "u" }, kids)).toEqual([]);
  });
});

describe("splitSharedFiles", () => {
  it("lifts the trailing shared and not-shared lines out of a report", () => {
    const r = splitSharedFiles(
      "Done.\n\nShared with the fleet (read_work): a.md, notes/b.md.\nAlso changed, not shared (binary or too large): x.png, y.bin.",
    );
    expect(r).toEqual({ body: "Done.", shared: ["a.md", "notes/b.md"], unshared: ["x.png", "y.bin"] });
  });

  it("either line alone", () => {
    expect(splitSharedFiles("ok\n\nShared with the fleet (read_work): notes/names.md.")).toEqual({
      body: "ok",
      shared: ["notes/names.md"],
      unshared: [],
    });
    expect(splitSharedFiles("ok\nAlso changed, not shared (binary or too large): big.zip.").unshared).toEqual([
      "big.zip",
    ]);
  });

  it("keeps the phrase when it is not at the end", () => {
    const text = "Shared with the fleet (read_work): a.md.\n\nThen I wrote more.";
    expect(splitSharedFiles(text)).toEqual({ body: text, shared: [], unshared: [] });
  });

  it("text without the lines is untouched", () => {
    expect(splitSharedFiles("plain")).toEqual({ body: "plain", shared: [], unshared: [] });
  });
});

describe("parsePublished", () => {
  it("reads the server's line", () => {
    expect(parsePublished('Published file "notes/names.md" (version 1, 923 bytes)')).toEqual({
      kind: "file",
      name: "notes/names.md",
      version: 1,
      bytes: 923,
    });
  });
  it("unquotes Go escapes", () => {
    expect(parsePublished('Published app "a \\"b\\".html" (version 12, 0 bytes)')?.name).toBe('a "b".html');
  });
  it("anything else is not a publish line", () => {
    expect(parsePublished("Published a thing")).toBeNull();
  });
});

describe("findWorkItem", () => {
  const w = (id: string, name: string, kind = "file", parent_id = "", updated_at = "2026-09-01") => ({
    id,
    name,
    kind,
    parent_id,
    updated_at,
  });
  const items = [
    w("ws", "fleet-notes", "workspace"),
    w("in", "README.md", "file", "ws"),
    w("dup1", "notes/a.md", "file", "ws", "2026-09-02"),
    w("dup2", "notes/a.md", "file", "ws2", "2026-09-05"),
    w("top", "notes/names.md"),
    w("nested", "notes/names.md", "file", "ws"),
  ];
  it("prefers a top-level item with the exact name", () => {
    expect(findWorkItem(items, "notes/names.md")?.id).toBe("top");
  });
  it("then the newest exact name anywhere", () => {
    expect(findWorkItem(items, "notes/a.md")?.id).toBe("dup2");
  });
  it("then a walk through folders", () => {
    expect(findWorkItem(items, "fleet-notes/README.md")?.id).toBe("in");
    expect(findWorkItem(items, "fleet-notes\\README.md")?.id).toBe("in");
  });
  it("nothing for a name the catalog does not have", () => {
    expect(findWorkItem(items, "missing.md")).toBeUndefined();
    expect(findWorkItem(items, "")).toBeUndefined();
  });
});
