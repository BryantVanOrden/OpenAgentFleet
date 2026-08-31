#!/usr/bin/env python3
"""Example 5: MCP tool servers and multi-bot DAG pipelines.

Registration is real: the orchestrator connects, completes the MCP handshake
and lists the server's tools before storing anything — so a server that cannot
be reached raises here instead of sitting in the hub looking configured. (An
earlier version of this example "registered" the GitHub server with a whole
command line in the `command` field, against a bridge that spoke no protocol;
both halves have since been fixed, which is why this one handles failure.)

Two transports:

  http  — any Streamable-HTTP MCP server; the easy path, shown below.
  stdio — the orchestrator execs the command IN ITS OWN CONTAINER, which is
          Alpine with the Go binary and not much else. `npx ...` therefore
          fails unless you extend the orchestrator image with Node. Operators
          can disable stdio entirely with MCP_DISABLE_STDIO=true.
"""

from agentfleet import FleetClient
from agentfleet.exceptions import FleetApiError


def main():
    fleet = FleetClient("http://localhost:8080")
    # fleet.login("you@example.com", "...")   # registration is admin-only

    # 1. Register an MCP server over HTTP. Point this at a real one — anything
    #    speaking Streamable HTTP works; env entries become request headers,
    #    which is how a bearer token is passed (and why they are never echoed
    #    back: the API returns env_keys, names only).
    print("🔌 Registering an MCP tool server...")
    try:
        srv = fleet.register_mcp_server(
            name="my-tools",
            transport="http",
            url="http://host.docker.internal:9911/mcp",
            # env={"Authorization": "Bearer ..."},
        )
        print(f"✅ Connected: {srv['name']} exposes {srv['tools_count']} tool(s)")

        # 2. Call a tool. The server is resolved from the tool name.
        result = fleet.call_mcp_tool("add_numbers", {"a": 17, "b": 25})
        print(f"🔧 Tool said: {result.get('text')}")
    except FleetApiError as exc:
        # An unreachable server is refused, not stored — that is the contract.
        print(f"⚠️  Not registered (is a server running at that URL?): {exc}")

    # 3. Pipelines: independent stages run in parallel, and edges can carry
    #    conditions (success / failure / contains:... ) so a graph can have an
    #    error branch that only fires on an error. Build them in the console,
    #    or POST /api/pipelines.
    pipelines = fleet.list_pipelines()
    print(f"\n⛓️ {len(pipelines)} workflow pipeline(s) registered.")
    if pipelines:
        target = pipelines[0]
        print(f"🚀 Triggering '{target['name']}'...")
        run = fleet.run_pipeline(target["id"])
        print(f"✅ Run {run.get('id')} is {run.get('status')} — node states: {run.get('node_states')}")

    # 4. The cost of all of the above, per turn and in aggregate.
    fin = fleet.get_financial_summary()
    print("\n📊 Financial telemetry:")
    print(f"   Total spend:    ${fin.get('total_cost_usd', 0.0):.4f} USD")
    print(f"   Prompt tokens:  {fin.get('total_prompt_tokens', 0):,}"
          f" (cached: {fin.get('total_cached_tokens', 0):,})")
    print(f"   Avg latency:    {fin.get('avg_latency_ms', 0)} ms")


if __name__ == "__main__":
    main()
