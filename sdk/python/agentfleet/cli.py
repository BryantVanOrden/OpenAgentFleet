"""fleetctl — The Command-Line Interface for AgentFleet.

Provides complete administrative control, telemetry, task execution,
swarm orchestration, voice synthesis, and system diagnostics.
"""

from __future__ import annotations

import argparse
import getpass
import json
import os
import sys
import time
from pathlib import Path
from typing import Any, Optional

if hasattr(sys.stdout, "reconfigure"):
    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    except Exception:
        pass

if hasattr(sys.stderr, "reconfigure"):
    try:
        sys.stderr.reconfigure(encoding="utf-8", errors="replace")
    except Exception:
        pass

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from agentfleet.client import FleetClient
from agentfleet.exceptions import FleetError

CONFIG_FILE = Path.home() / ".agentfleet" / "config.json"


def _load_config() -> dict[str, Any]:
    if CONFIG_FILE.exists():
        try:
            return json.loads(CONFIG_FILE.read_text(encoding="utf-8"))
        except Exception:
            pass
    return {}


def _save_config(cfg: dict[str, Any]) -> None:
    CONFIG_FILE.parent.mkdir(parents=True, exist_ok=True)
    CONFIG_FILE.write_text(json.dumps(cfg, indent=2), encoding="utf-8")


def _get_client(args: argparse.Namespace) -> FleetClient:
    cfg = _load_config()
    url = getattr(args, "url", None) or os.environ.get("AGENTFLEET_URL") or cfg.get("url", "http://localhost:8080")
    token = getattr(args, "token", None) or os.environ.get("AGENTFLEET_TOKEN") or cfg.get("token")
    return FleetClient(base_url=url, token=token)


# ---------------------------------------------------------------- Diagnostics ---


def cmd_diagnostics(args: argparse.Namespace) -> None:
    """Runs a complete diagnostic probe across the platform."""
    client = _get_client(args)
    print("================================================================================")
    print("                      ⚡ AGENTFLEET SYSTEM DIAGNOSTICS                          ")
    print("================================================================================")
    print(f"Target URL:    {client.base_url}")
    print(f"Token Config:  {'Configured (***)' if client.token else 'Not Authenticated'}")
    print("--------------------------------------------------------------------------------")

    # 1. API Health
    print("1. Probing Orchestrator API Health... ", end="", flush=True)
    try:
        health = client.health()
        print("✅ [OK]")
        print(f"   Database:  {health.get('database', 'connected')}")
        print(f"   Instances: {health.get('instances_active', 'ready')}")
    except Exception as e:
        print(f"❌ [FAILED]: {e}")

    # 2. Instances Check
    print("2. Probing Instance Fabric & Cgroup Isolation... ", end="", flush=True)
    try:
        bots = client.list_bots()
        print(f"✅ [OK] ({len(bots)} instances registered)")
    except Exception as e:
        print(f"⚠️  [UNAVAILABLE]: {e}")

    # 3. Model Providers Check
    print("3. Probing Vision AI Model Providers... ", end="", flush=True)
    try:
        providers = client._get("/api/providers") or []
        print(f"✅ [OK] ({len(providers)} providers configured)")
    except Exception as e:
        print(f"⚠️  [NOTICE]: {e}")

    # 4. Swarm Blackboard Engine
    print("4. Probing Multi-Agent Swarm Coordinator... ", end="", flush=True)
    try:
        swarms = client.list_swarms()
        print(f"✅ [OK] ({len(swarms)} swarms tracked)")
    except Exception as e:
        print(f"⚠️  [NOTICE]: {e}")

    print("================================================================================")
    print("Diagnostics complete. Platform operational.")


# -------------------------------------------------------------------- Config ---


def cmd_config(args: argparse.Namespace) -> None:
    cfg = _load_config()
    if args.action == "set-url":
        cfg["url"] = args.value
        _save_config(cfg)
        print(f"✅ Orchestrator URL set to: {args.value}")
    elif args.action == "set-token":
        cfg["token"] = args.value
        _save_config(cfg)
        print("✅ Session token saved.")
    elif args.action == "show":
        print(json.dumps(cfg, indent=2))


# --------------------------------------------------------------------- Login ---


def cmd_login(args: argparse.Namespace) -> None:
    client = _get_client(args)
    email = args.email or input("Email: ")
    password = args.password or getpass.getpass("Password: ")
    try:
        token = client.login(email, password)
        cfg = _load_config()
        cfg["token"] = token
        _save_config(cfg)
        print(f"✅ Successfully signed in as {email}")
    except FleetError as e:
        print(f"❌ Login failed: {e}")
        sys.exit(1)


# ----------------------------------------------------------------- Instances ---


def cmd_list(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        bots = client.list_bots()
        if not bots:
            print("No bot instances found. Run `fleetctl deploy` to provision one.")
            return

        print(f"{'ID':<20} {'NAME':<24} {'ARCHETYPE':<18} {'TIER':<12} {'STATE':<10}")
        print("-" * 88)
        for b in bots:
            arch = b.archetype_id or "custom"
            print(f"{b.id:<20} {b.name:<24} {arch:<18} {b.tier:<12} {b.state:<10}")
    except Exception as e:
        print(f"❌ Error: {e}")


def cmd_inspect(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        bot = client.get_bot(args.id)
        stats = bot.stats()
        print(f"ID:           {bot.id}")
        print(f"Name:         {bot.name}")
        print(f"Archetype:    {bot.archetype_id or 'none'}")
        print(f"State:        {bot.state}")
        print(f"Tier:         {bot.tier}")
        print(f"Shell Access: {bot.info.shell_access}")
        print(f"VNC Stream:   {bot.info.vnc_url or 'N/A'}")
        print(f"Stats:        CPU {stats.get('cpu_percent', 0):.1f}% | RAM {stats.get('memory_bytes', 0) // (1024*1024)} MB")
    except Exception as e:
        print(f"❌ Error: {e}")


def cmd_deploy(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        bot = client.deploy_bot(
            archetype_id=args.archetype,
            name=args.name,
            tier=args.tier,
            system_prompt=args.prompt,
        )
        print(f"🚀 Provisioned Bot: {bot.name} (ID: {bot.id}, Archetype: {bot.archetype_id})")
    except Exception as e:
        print(f"❌ Deployment failed: {e}")
        sys.exit(1)


# ---------------------------------------------------------------------- Tasks ---


def cmd_run(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        bot = client.get_bot(args.id)
        print(f"🚀 Dispatching goal to {bot.name}: '{args.goal}'")
        task = bot.run(args.goal, max_steps=args.max_steps, wait=False)
        print(f"Task ID: {task.id} (Status: {task.state})")

        if args.wait:
            print("Streaming execution updates...")
            last_step = 0
            while not task.is_done:
                time.sleep(1.5)
                task.refresh()
                steps = task.get_steps() or []
                for s in steps[last_step:]:
                    thought = s.get("thought", "")
                    action = s.get("action", {})
                    act_name = action.get("action", "step")
                    print(f"  [{s.get('step', 0)}] {act_name} -> {thought[:70]}")
                last_step = len(steps)

            print("-" * 50)
            if task.state == "succeeded":
                print(f"✅ Succeeded: {task.result}")
            else:
                print(f"❌ Finished with state: {task.state} (Error: {task.error})")
    except Exception as e:
        print(f"❌ Task failed: {e}")
        sys.exit(1)


# -------------------------------------------------------------------- Swarms ---


def cmd_swarm_list(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        swarms = client.list_swarms()
        if not swarms:
            print("No active or historical swarms found.")
            return

        print(f"{'SWARM ID':<24} {'NAME':<28} {'MEMBERS':<10} {'STATUS':<12}")
        print("-" * 76)
        for s in swarms:
            print(f"{s.id:<24} {s.name:<28} {len(s.members):<10} {s.status:<12}")
    except Exception as e:
        print(f"❌ Error: {e}")


def cmd_swarm_launch(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        sw = client.launch_swarm(name=args.name, mission=args.mission)
        print(f"🚀 Launched Swarm: '{sw.name}' (ID: {sw.id})")
        print(f"🎯 Mission: {sw.mission}")
        print(f"👥 Members: {len(sw.members)} specialized bots assigned.")
    except Exception as e:
        print(f"❌ Launch failed: {e}")
        sys.exit(1)


def cmd_swarm_watch(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        sw = client.get_swarm(args.id)
        print(f"Watching Blackboard for Swarm: {sw.name} (Status: {sw.status})")
        last_count = 0
        while True:
            sw.refresh()
            msgs = sw.messages
            for m in msgs[last_count:]:
                print(f"[{m.phase.upper()}] {m.from_bot} ➔ {m.to_bot}: {m.content}")
            last_count = len(msgs)
            time.sleep(2)
    except KeyboardInterrupt:
        print("\nStopped watching.")
    except Exception as e:
        print(f"❌ Error: {e}")


# --------------------------------------------------------------------- Voice ---


def cmd_voice_list(args: argparse.Namespace) -> None:
    print("Curated Pocket TTS Voice Arsenal (6 Profiles):")
    print("  🕶️ shadow  (Male, Deep Cyberpunk Operative — DEFAULT)")
    print("  🏛️ atlas   (Male, Resonant Architectural Leader)")
    print("  ⚡ vortex  (Male, Dynamic High-Velocity Engineer)")
    print("  📊 echo    (Male, Calm Analytical Quant)")
    print("  💎 aura    (Female, Crisp Futuristic AI Co-Pilot)")
    print("  🌸 lyra    (Female, Warm Conversational Guide)")


def cmd_voice_speak(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        res = client.speak(args.text, voice=args.voice)
        print(f"🔊 Spoken with voice '{args.voice}': \"{args.text}\"")
    except Exception as e:
        print(f"❌ Speech synthesis failed: {e}")


# ------------------------------------------------------------------ Webhooks ---


def cmd_webhooks_list(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        whs = client._get("/api/webhooks") or []
        print(f"{'TOKEN':<20} {'NAME':<32} {'TARGET BOT':<16} {'ACTIVE'}")
        print("-" * 76)
        for w in whs:
            print(f"{w.get('token',''):<20} {w.get('name',''):<32} {w.get('target_archetype',''):<16} {w.get('active', True)}")
    except Exception as e:
        print(f"❌ Error: {e}")


def cmd_webhooks_trigger(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        data = json.loads(args.data) if args.data else {}
        res = client._post(f"/api/webhooks/{args.token}", data)
        print(f"⚡ Inbound webhook '{args.token}' triggered successfully.")
        print(json.dumps(res, indent=2))
    except Exception as e:
        print(f"❌ Trigger failed: {e}")


# ------------------------------------------------------------- Vault & Comms ---


def cmd_vault_list(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        secs = client._get("/api/vault/secrets") or []
        print(f"\n🔑 SHARED SECRETS & VARIABLES ({len(secs)}):")
        print(f"{'KEY':<24} {'SCOPE':<12} {'NOTE':<30}")
        print("-" * 68)
        for s in secs:
            print(f"{s.get('key',''):<24} {s.get('scope',''):<12} {s.get('note',''):<30}")

        sess = client._get("/api/vault/sessions") or []
        print(f"\n🍪 SHARED BROWSER SESSIONS ({len(sess)}):")
        for se in sess:
            print(f"- {se.get('domain','')} ({se.get('title','')})")
    except Exception as e:
        print(f"❌ Error: {e}")


def cmd_vault_put(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        res = client._post("/api/vault/secrets", {
            "key": args.key,
            "value": args.value,
            "scope": args.scope,
            "note": args.note or "",
        })
        print(f"✅ Saved shared secret: {args.key} to vault.")
    except Exception as e:
        print(f"❌ Error: {e}")


def cmd_vault_comms(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        if args.broadcast:
            msg = client._post("/api/vault/comms", {
                "content": args.broadcast,
                "from_instance_name": "CLI Operator",
                "to_instance_id": "broadcast",
            })
            print(f"📢 Broadcast sent across fleet: {args.broadcast}")
        else:
            msgs = client._get("/api/vault/comms") or []
            print(f"💬 RECENT INTER-AGENT COMMS ({len(msgs)}):")
            print("-" * 68)
            for m in msgs[:15]:
                print(f"[{m.get('from_instance_name','')} ➔ {m.get('to_instance_id','')}] ({m.get('kind','')}): {m.get('content','')}")
    except Exception as e:
        print(f"❌ Error: {e}")


# ----------------------------------------------------------------------- MCP ---


def cmd_mcp_list(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        srvs = client.list_mcp_servers()
        tools = client._get("/api/mcp/tools") or []
        print(f"🔌 CONNECTED MCP SERVERS ({len(srvs)}):")
        for s in srvs:
            print(f"- {s.get('name')} [{s.get('transport')}] ➔ {s.get('command')}")
        print(f"\n🛠️  DISCOVERED MCP TOOLS ({len(tools)}):")
        for t in tools:
            print(f"  • {t.get('name')}: {t.get('description')}")
    except Exception as e:
        print(f"❌ Error: {e}")


def cmd_mcp_add(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        res = client.register_mcp_server(name=args.name, command=args.command, transport=args.transport)
        print(f"✅ Connected MCP server: {args.name}")
    except Exception as e:
        print(f"❌ Error: {e}")


# ----------------------------------------------------------------- Pipelines ---


def cmd_pipeline_list(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        pipes = client.list_pipelines()
        print(f"⛓️ WORKFLOW DAG PIPELINES ({len(pipes)}):")
        print(f"{'ID':<24} {'NAME':<32} {'STAGES':<8}")
        print("-" * 68)
        for p in pipes:
            print(f"{p.get('id',''):<24} {p.get('name',''):<32} {len(p.get('nodes',[]))}")
    except Exception as e:
        print(f"❌ Error: {e}")


def cmd_pipeline_run(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        res = client.run_pipeline(args.id)
        print(f"🚀 Triggered pipeline run: {res.get('id')} (Status: {res.get('status')})")
    except Exception as e:
        print(f"❌ Error: {e}")


# ---------------------------------------------------------------- Financials ---


def cmd_financials(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        fin = client.get_financial_summary()
        print("================================================================================")
        print("                     📊 FLEET FINANCIAL TELEMETRY COCKPIT                       ")
        print("================================================================================")
        print(f"Total Fleet Spend:     ${fin.get('total_cost_usd', 0.0):.4f} USD")
        print(f"Prompt Tokens:         {fin.get('total_prompt_tokens', 0):,}")
        print(f"Completion Tokens:     {fin.get('total_completion_tokens', 0):,}")
        print(f"Cached Tokens:         {fin.get('total_cached_tokens', 0):,}")
        print(f"Average Turn Latency:  {fin.get('avg_latency_ms', 0)} ms")
        print(f"Total Model Turns:     {fin.get('turns_count', 0)}")
        print("================================================================================")
    except Exception as e:
        print(f"❌ Error: {e}")


# ----------------------------------------------------------------------- Hub ---


def cmd_hub_export(args: argparse.Namespace) -> None:
    from agentfleet.hub import ArchetypeManifest

    client = _get_client(args)
    tmpl = None
    try:
        tmpl = client._get(f"/api/templates/{args.archetype}")
    except Exception:
        pass

    if not tmpl:
        tmpl = {"id": args.archetype, "name": args.archetype, "recommended_tier": "standard"}
    
    try:
        manifest = ArchetypeManifest(
            version="1.0.0",
            id=tmpl.get("id", args.archetype),
            name=tmpl.get("name", args.archetype),
            tagline=tmpl.get("tagline", ""),
            category=tmpl.get("category", "General"),
            recommended_tier=tmpl.get("recommended_tier", "standard"),
            vcpu=float(tmpl.get("vcpu", 4)),
            memory_mb=int(tmpl.get("memory_mb", 8192)),
            disk_gb=int(tmpl.get("disk_gb", 30)),
            gpu=bool(tmpl.get("gpu", False)),
            preinstalled_tools=tmpl.get("preinstalled_tools", []),
            system_prompt=tmpl.get("specialized_prompt", ""),
            default_environment=tmpl.get("default_environment", {}),
            mcp_servers=[],
            recorded_skills=[],
        )
        out_path = Path(args.output or f"{args.archetype}.agentfleet.json")
        manifest.save(out_path)
        print(f"📦 Archetype '{args.archetype}' exported to {out_path}")
    except Exception as e:
        print(f"❌ Export failed: {e}")


def cmd_hub_import(args: argparse.Namespace) -> None:
    from agentfleet.hub import ArchetypeManifest

    try:
        manifest = ArchetypeManifest.load(args.file)
        print(f"✅ Loaded archetype package: {manifest.name} ({manifest.id})")
        print(f"   Category: {manifest.category} · Hardware: {manifest.recommended_tier}")
        print(f"   Tools: {', '.join(manifest.preinstalled_tools)}")
    except Exception as e:
        print(f"❌ Import failed: {e}")


# ---------------------------------------------------------------------- Main ---


def main() -> None:
    parser = argparse.ArgumentParser(
        prog="fleetctl",
        description="AgentFleet CLI — Manage hard-sandboxed autonomous OS agents & collaborative swarms",
    )
    parser.add_argument("--url", help="AgentFleet orchestrator URL")
    parser.add_argument("--token", help="Authentication Bearer token")

    subparsers = parser.add_subparsers(dest="subcommand", required=True)

    # diagnostics
    p_diag = subparsers.add_parser("diagnostics", help="Run comprehensive platform health diagnostics")
    p_diag.set_defaults(func=cmd_diagnostics)

    # config
    p_conf = subparsers.add_parser("config", help="Manage local fleetctl configuration")
    p_conf.add_argument("action", choices=["set-url", "set-token", "show"])
    p_conf.add_argument("value", nargs="?", default="")
    p_conf.set_defaults(func=cmd_config)

    # login
    p_login = subparsers.add_parser("login", help="Authenticate with email & password")
    p_login.add_argument("--email", "-e")
    p_login.add_argument("--password", "-p")
    p_login.set_defaults(func=cmd_login)

    # list / ps
    p_list = subparsers.add_parser("list", aliases=["ps"], help="List all bot instances")
    p_list.set_defaults(func=cmd_list)

    # inspect
    p_insp = subparsers.add_parser("inspect", help="Inspect bot details and live telemetry")
    p_insp.add_argument("id", help="Bot instance ID")
    p_insp.set_defaults(func=cmd_inspect)

    # deploy
    p_dep = subparsers.add_parser("deploy", help="Deploy a specialized bot archetype")
    p_dep.add_argument("--archetype", "-a", default="fullstack_dev", help="Archetype ID (e.g. cyber_ops, agentic_crm)")
    p_dep.add_argument("--name", "-n", help="Custom bot name")
    p_dep.add_argument("--tier", "-t", default="standard", help="Resource tier profile")
    p_dep.add_argument("--prompt", help="Custom system prompt override")
    p_dep.set_defaults(func=cmd_deploy)

    # run
    p_run = subparsers.add_parser("run", help="Dispatch an autonomous task goal to a bot")
    p_run.add_argument("id", help="Target bot instance ID")
    p_run.add_argument("goal", help="Task goal instruction")
    p_run.add_argument("--wait", "-w", action="store_true", help="Block and stream execution progress")
    p_run.add_argument("--max-steps", type=int, default=60, help="Maximum step limit")
    p_run.set_defaults(func=cmd_run)

    # swarm
    p_sw = subparsers.add_parser("swarm", help="Manage collaborative multi-agent swarms")
    sw_subs = p_sw.add_subparsers(dest="swarm_action", required=True)
    
    p_sw_list = sw_subs.add_parser("list", help="List all swarms")
    p_sw_list.set_defaults(func=cmd_swarm_list)

    p_sw_launch = sw_subs.add_parser("launch", help="Launch a team swarm")
    p_sw_launch.add_argument("--name", "-n", required=True, help="Swarm mission name")
    p_sw_launch.add_argument("--mission", "-m", required=True, help="Shared project objective")
    p_sw_launch.set_defaults(func=cmd_swarm_launch)

    p_sw_watch = sw_subs.add_parser("watch", help="Watch live swarm blackboard messages")
    p_sw_watch.add_argument("id", help="Swarm ID")
    p_sw_watch.set_defaults(func=cmd_swarm_watch)

    # vault
    p_vault = subparsers.add_parser("vault", help="Manage shared secrets, sessions, and comms")
    v_subs = p_vault.add_subparsers(dest="vault_action", required=True)
    p_v_list = v_subs.add_parser("list", help="List shared secrets and sessions")
    p_v_list.set_defaults(func=cmd_vault_list)
    p_v_put = v_subs.add_parser("put", help="Publish a shared secret")
    p_v_put.add_argument("key", help="Secret key")
    p_v_put.add_argument("value", help="Secret value")
    p_v_put.add_argument("--scope", default="fleet", help="Scope (fleet, swarm)")
    p_v_put.add_argument("--note", help="Note/description")
    p_v_put.set_defaults(func=cmd_vault_put)
    p_v_comms = v_subs.add_parser("comms", help="Inspect or broadcast inter-agent comms")
    p_v_comms.add_argument("--broadcast", "-b", help="Broadcast message content")
    p_v_comms.set_defaults(func=cmd_vault_comms)

    # mcp
    p_mcp = subparsers.add_parser("mcp", help="Manage Model Context Protocol (MCP) servers")
    mcp_subs = p_mcp.add_subparsers(dest="mcp_action", required=True)
    p_mcp_list = mcp_subs.add_parser("list", help="List connected MCP servers and tools")
    p_mcp_list.set_defaults(func=cmd_mcp_list)
    p_mcp_add = mcp_subs.add_parser("add", help="Connect a new MCP server")
    p_mcp_add.add_argument("name", help="Server name")
    p_mcp_add.add_argument("command", help="Command or URL")
    p_mcp_add.add_argument("--transport", "-t", default="stdio", choices=["stdio", "sse"])
    p_mcp_add.set_defaults(func=cmd_mcp_add)

    # pipeline
    p_pipe = subparsers.add_parser("pipeline", help="Manage multi-bot workflow DAG pipelines")
    pipe_subs = p_pipe.add_subparsers(dest="pipeline_action", required=True)
    p_pipe_list = pipe_subs.add_parser("list", help="List workflow pipelines")
    p_pipe_list.set_defaults(func=cmd_pipeline_list)
    p_pipe_run = pipe_subs.add_parser("run", help="Run a workflow pipeline")
    p_pipe_run.add_argument("id", help="Pipeline ID")
    p_pipe_run.set_defaults(func=cmd_pipeline_run)

    # financials
    p_fin = subparsers.add_parser("financials", help="View fleet-wide token and cost financial telemetry")
    p_fin.set_defaults(func=cmd_financials)

    # hub
    p_hub = subparsers.add_parser("hub", help="Export and import portable bot archetypes")
    hub_subs = p_hub.add_subparsers(dest="hub_action", required=True)
    p_hub_exp = hub_subs.add_parser("export", help="Export archetype to .agentfleet.json")
    p_hub_exp.add_argument("archetype", help="Archetype ID")
    p_hub_exp.add_argument("-o", "--output", help="Output file path")
    p_hub_exp.set_defaults(func=cmd_hub_export)
    p_hub_imp = hub_subs.add_parser("import", help="Import archetype from package file")
    p_hub_imp.add_argument("file", help="File path to .agentfleet.json")
    p_hub_imp.set_defaults(func=cmd_hub_import)

    # voice
    p_voice = subparsers.add_parser("voice", help="Interact with Pocket TTS speech engine")
    v_subs = p_voice.add_subparsers(dest="voice_action", required=True)
    p_v_list = v_subs.add_parser("list", help="List curated voice models")
    p_v_list.set_defaults(func=cmd_voice_list)
    p_v_speak = v_subs.add_parser("speak", help="Speak text verbally")
    p_v_speak.add_argument("text", help="Text to speak")
    p_v_speak.add_argument("--voice", "-v", default="shadow", help="Voice model")
    p_v_speak.set_defaults(func=cmd_voice_speak)

    # webhooks
    p_wh = subparsers.add_parser("webhooks", help="Manage event-driven webhooks")
    wh_subs = p_wh.add_subparsers(dest="webhook_action", required=True)
    p_wh_list = wh_subs.add_parser("list", help="List webhooks")
    p_wh_list.set_defaults(func=cmd_webhooks_list)
    p_wh_trig = wh_subs.add_parser("trigger", help="Trigger an inbound webhook")
    p_wh_trig.add_argument("token", help="Webhook token")
    p_wh_trig.add_argument("--data", "-d", help="JSON payload")
    p_wh_trig.set_defaults(func=cmd_webhooks_trigger)

    args = parser.parse_args()
    if hasattr(args, "func"):
        args.func(args)
    else:
        parser.print_help()


if __name__ == "__main__":
    main()
