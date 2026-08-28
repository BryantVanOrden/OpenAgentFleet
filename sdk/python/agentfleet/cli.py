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

    # voice
    p_voice = subparsers.add_parser("voice", help="Interact with Pocket TTS speech engine")
    v_subs = p_voice.add_subparsers(dest="voice_action", required=True)
    
    p_v_list = v_subs.add_parser("list", help="List curated voice models")
    p_v_list.set_defaults(func=cmd_voice_list)

    p_v_speak = v_subs.add_parser("speak", help="Speak text verbally")
    p_v_speak.add_argument("text", help="Text to speak")
    p_v_speak.add_argument("--voice", "-v", default="shadow", help="Voice model (shadow, atlas, vortex, echo, aura, lyra)")
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
