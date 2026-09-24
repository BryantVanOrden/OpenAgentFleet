import { describe, expect, it } from "vitest";
import type { TicketView } from "./api";
import {
  agentStatus,
  budgetFraction,
  filterTickets,
  groupByStatus,
  initials,
  money,
  openBlockerCount,
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
