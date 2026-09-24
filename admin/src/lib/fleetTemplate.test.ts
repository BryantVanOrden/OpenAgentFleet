import { describe, expect, it } from "vitest";
import { createdKinds } from "./fleetTemplate";

describe("createdKinds", () => {
  const template = {
    agents: [
      { name: "Research hook", kind: "webhook" as const },
      { name: "Claude", kind: "claude_code" as const },
      { name: "Builder", kind: "desktop" as const },
    ],
  };

  it("names each new agent's kind, through a rename", () => {
    expect(
      createdKinds(template, {
        created: ["Research hook", "Claude 2", "Builder 2"],
        renamed: ["Claude → Claude 2", "Builder → Builder 2"],
      }),
    ).toEqual([
      { name: "Research hook", kind: "webhook" },
      { name: "Claude 2", kind: "claude_code" },
      { name: "Builder 2", kind: "desktop" },
    ]);
  });

  it("an agent it cannot place is counted as a desktop, the kind that costs a machine", () => {
    expect(createdKinds(template, { created: ["Mystery"], renamed: [] })).toEqual([{ name: "Mystery", kind: "desktop" }]);
  });
});
