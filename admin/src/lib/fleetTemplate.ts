import type { AgentKind, FleetTemplate, ImportResult } from "./api";

/**
 * The template's own entry for each agent an import would create, so the
 * preview can say what kind each is. A renamed agent is listed by its new
 * name ("Builder 2") in `created` and as "Builder → Builder 2" in `renamed`.
 */
export function createdKinds(
  template: Pick<FleetTemplate, "agents">,
  result: Pick<ImportResult, "created" | "renamed">,
): { name: string; kind: AgentKind }[] {
  const byName = new Map(template.agents.map((a) => [a.name, a.kind]));
  const renamedFrom = new Map<string, string>();
  for (const r of result.renamed) {
    const [from, to] = r.split(/\s*→\s*/);
    if (from && to) renamedFrom.set(to.trim(), from.trim());
  }
  return result.created.map((name) => ({
    name,
    kind: byName.get(name) ?? byName.get(renamedFrom.get(name) ?? "") ?? "desktop",
  }));
}
