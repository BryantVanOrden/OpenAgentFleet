import { describe, expect, it } from "vitest";
import type { FleetCommand, Instance, PeerMessage, Task } from "./api";
import {
  commandPrefix,
  commandText,
  filterCommands,
  fleetSummary,
  formatFleetSummary,
  isSpokenMessage,
} from "./fleetChat";

const catalogue: FleetCommand[] = [
  { name: "bots", usage: "/bots", description: "List every agent and its state", mutates: false },
  { name: "status", usage: "/status", description: "Fleet health at a glance", mutates: false },
  { name: "alerts", usage: "/alerts", description: "Open alerts needing a reply", mutates: false },
  { name: "mission", usage: "/mission <name> <goal>", description: "Start a mission", mutates: true },
  { name: "missions", usage: "/missions", description: "Missions and their status", mutates: false },
  { name: "approve", usage: "/approve <id>", description: "Approve a mission artifact", mutates: true },
  { name: "help", usage: "/help", description: "What the bots can do", mutates: false },
];

describe("filterCommands", () => {
  it("an empty prefix is the whole catalogue, in order", () => {
    expect(filterCommands(catalogue, "").map((c) => c.name)).toEqual(catalogue.map((c) => c.name));
  });

  it("ranks name-prefix hits above substring and description hits", () => {
    // "st" starts /status; "bots" and "missions" only mention it in their
    // descriptions ("state", "status"); "help" does not match at all.
    const names = filterCommands(catalogue, "st").map((c) => c.name);
    expect(names[0]).toBe("status");
    expect(names).toContain("missions");
    expect(names).not.toContain("help");
  });

  it("matches on usage and description too, case-insensitively", () => {
    expect(filterCommands(catalogue, "GOAL").map((c) => c.name)).toEqual(["mission"]);
  });

  it("tolerates a leading slash in the prefix", () => {
    const withSlash = filterCommands(catalogue, "/he").map((c) => c.name);
    expect(withSlash).toEqual(filterCommands(catalogue, "he").map((c) => c.name));
    expect(withSlash[0]).toBe("help");
  });

  it("keeps catalogue order within a rank", () => {
    expect(filterCommands(catalogue, "mission").map((c) => c.name)).toEqual([
      "mission",
      "missions",
      "approve",
    ]);
  });

  it("returns nothing for nonsense", () => {
    expect(filterCommands(catalogue, "zzz")).toEqual([]);
  });
});

describe("commandPrefix", () => {
  it("is the text after the slash while no argument has started", () => {
    expect(commandPrefix("/")).toBe("");
    expect(commandPrefix("/bo")).toBe("bo");
  });

  it("closes once a space or newline follows the command", () => {
    expect(commandPrefix("/mission ")).toBeNull();
    expect(commandPrefix("/mission\n")).toBeNull();
  });

  it("ignores text that is not a command", () => {
    expect(commandPrefix("hello /bots")).toBeNull();
    expect(commandPrefix("")).toBeNull();
  });
});

describe("commandText", () => {
  it("adds the slash whether or not the server did", () => {
    expect(commandText({ name: "bots", usage: "bots", description: "", mutates: false })).toEqual({
      label: "/bots",
      usage: "/bots",
    });
    expect(commandText({ name: "/bots", usage: "/bots", description: "", mutates: false })).toEqual({
      label: "/bots",
      usage: "/bots",
    });
  });

  it("falls back to the label when usage is blank", () => {
    expect(commandText({ name: "help", usage: "", description: "", mutates: false }).usage).toBe(
      "/help",
    );
  });
});

const inst = (id: string) => ({ id, name: id }) as Instance;
const task = (instance_id: string, state: Task["state"]) =>
  ({ id: `${instance_id}-${state}`, instance_id, state }) as Task;

describe("fleetSummary", () => {
  it("counts agents and distinct working agents", () => {
    const s = fleetSummary(
      [inst("a"), inst("b"), inst("c")],
      [task("a", "running"), task("a", "running"), task("b", "queued"), task("c", "succeeded")],
    );
    expect(s).toEqual({ agents: 3, working: 1 });
  });

  it("does not count a running task on an agent that no longer exists", () => {
    expect(fleetSummary([inst("a")], [task("ghost", "running")])).toEqual({
      agents: 1,
      working: 0,
    });
  });

  it("formats with the right plural", () => {
    expect(formatFleetSummary({ agents: 1, working: 0 })).toBe("1 agent · 0 working");
    expect(formatFleetSummary({ agents: 3, working: 2 })).toBe("3 agents · 2 working");
  });
});

describe("isSpokenMessage", () => {
  const msg = (over: Partial<PeerMessage>): PeerMessage => ({
    id: "m",
    from_instance_id: "bot-1",
    from_instance_name: "Scout",
    to_instance_id: "broadcast",
    kind: "reply",
    content: "done",
    created_at: "2026-01-01T00:00:00Z",
    ...over,
  });

  it("speaks agent replies, messages, questions and delegations", () => {
    for (const kind of ["reply", "message", "question", "delegation"]) {
      expect(isSpokenMessage(msg({ kind }))).toBe(true);
    }
  });

  it("stays quiet for you, for Oaf, and for compaction markers", () => {
    expect(isSpokenMessage(msg({ from_instance_id: "" }))).toBe(false);
    expect(isSpokenMessage(msg({ from_instance_id: "system", kind: "system" }))).toBe(false);
    expect(isSpokenMessage(msg({ kind: "summary" }))).toBe(false);
  });
});
