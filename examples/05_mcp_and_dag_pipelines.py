#!/usr/bin/env python3
"""Example 5: Connect MCP Tool Servers and execute a Multi-Bot Workflow DAG Pipeline."""

from agentfleet import FleetClient


def main():
    # 1. Connect to AgentFleet orchestrator
    fleet = FleetClient("http://localhost:8080")

    print("🔌 Registering Model Context Protocol (MCP) tool servers...")
    # Mount GitHub MCP server
    fleet.register_mcp_server(
        name="github",
        command="npx -y @modelcontextprotocol/server-github",
        transport="stdio",
    )
    print("✅ Mounted GitHub MCP server tools.")

    # 2. Inspect available Multi-Bot Workflow DAG Pipelines
    pipelines = fleet.list_pipelines()
    print(f"\n⛓️ Found {len(pipelines)} workflow pipelines registered.")

    # 3. If a pipeline exists, trigger multi-agent sequential execution
    if pipelines:
        target = pipelines[0]
        print(f"🚀 Triggering pipeline '{target['name']}'...")
        run = fleet.run_pipeline(target["id"])
        print(f"✅ Pipeline Run ID: {run.get('id')} (Status: {run.get('status')})")

    # 4. Inspect financial telemetry cockpit
    fin = fleet.get_financial_summary()
    print("\n📊 Real-Time Financial Telemetry:")
    print(f"   Total Spend:      ${fin.get('total_cost_usd', 0.0):.4f} USD")
    print(f"   Prompt Tokens:    {fin.get('total_prompt_tokens', 0):,}")
    print(f"   Avg Latency:      {fin.get('avg_latency_ms', 0)} ms")


if __name__ == "__main__":
    main()
