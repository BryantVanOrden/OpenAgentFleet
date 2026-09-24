"""fleetctl — The Command-Line Interface for OpenAgentFleet.

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

        print(f"{'ID':<36} {'NAME':<24} {'KIND':<12} {'ARCHETYPE':<18} {'TIER':<12} {'STATE':<10}")
        print("-" * 116)
        for b in bots:
            info = b.info
            arch = b.archetype_id or ("-" if info.external else "custom")
            tier = "-" if info.external else b.tier
            print(f"{b.id:<36} {b.name:<24} {info.kind:<12} {arch:<18} {tier:<12} {b.state:<10}")
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


# ------------------------------------------------------------- AI Models & Fallback ---


def cmd_models_list(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        providers = client.list_providers()
        print(f"\n🧠 AI CONNECTIONS & TIERED FALLBACK CHAIN ({len(providers)}):")
        print(f"{'TIER':<8} {'NAME':<24} {'KIND':<12} {'MODEL':<28} {'STATUS'}")
        print("-" * 80)
        for idx, p in enumerate(providers):
            tier_str = f"Tier {idx+1}"
            if idx == 0:
                tier_str += " ★"
            status = "Enabled" if p.get("enabled", True) else "Disabled"
            if p.get("vision"):
                status += " (Vision)"
            print(f"{tier_str:<8} {p.get('name',''):<24} {p.get('kind',''):<12} {p.get('model',''):<28} {status}")
    except Exception as e:
        print(f"❌ Error: {e}")


def cmd_models_reorder(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        updated = client.reorder_providers(args.ids)
        print(f"✅ Fallback sequence updated successfully across {len(updated)} engines:")
        for idx, p in enumerate(updated):
            role = "Primary Engine" if idx == 0 else f"Fallback #{idx}"
            print(f"  Tier {idx+1} ({role}): {p.get('name')} [{p.get('model')}]")
    except Exception as e:
        print(f"❌ Reorder failed: {e}")


def cmd_models_probe(args: argparse.Namespace) -> None:
    client = _get_client(args)
    try:
        res = client._post(f"/api/providers/{args.id}/probe")
        if res.get("ok"):
            print(f"✅ Provider '{args.id}' is reachable and authenticated.")
        else:
            print(f"❌ Provider '{args.id}' check failed: {res.get('error')}")
    except Exception as e:
        print(f"❌ Probe error: {e}")


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
    """Export an archetype as a portable package.

    The manifest comes from the orchestrator's exporter rather than being
    assembled here from the template catalogue. That is the difference between a
    package and a description: the fleet's recorded skills and its registered
    MCP servers are what make an archetype worth moving between installations,
    and the SDK cannot know either of them. Both used to be hardcoded to [].
    """
    from agentfleet.hub import ArchetypeManifest

    client = _get_client(args)
    path = f"/api/archetypes/{args.archetype}/export"
    if args.skills:
        path += f"?skills={args.skills}"

    try:
        data = client._get(path)
    except Exception as e:
        print(f"❌ Export failed: {e}")
        return

    try:
        manifest = ArchetypeManifest.from_dict(data)
        # .yaml by default, which is what this module and the docs have always
        # been named after. save() honours the extension, so `-o x.json` still
        # writes JSON.
        out_path = Path(args.output or f"{args.archetype}.agentfleet.yaml")
        manifest.save(out_path)
    except Exception as e:
        print(f"❌ Export failed: {e}")
        return

    print(f"📦 Archetype '{manifest.id}' exported to {out_path}")
    print(f"   {manifest.name} · {manifest.category} · tier {manifest.recommended_tier}")
    print(f"   Tools: {', '.join(manifest.preinstalled_tools) or 'none'}")
    print(f"   Recorded skills: {len(manifest.recorded_skills)}")
    print(f"   MCP servers: {len(manifest.mcp_servers)}")
    # Said out loud, because an operator who mails this file to a colleague
    # should know the colleague will have to supply the keys themselves.
    needs = [
        f"{s.get('name')}.{k}"
        for s in manifest.mcp_servers
        for k in (s.get("env_keys") or [])
    ]
    if needs:
        print(f"   ⚠️  Credentials NOT included (by design): {', '.join(needs)}")


def cmd_hub_import(args: argparse.Namespace) -> None:
    """Install an archetype package onto this fleet.

    This used to load the file, print three lines about it, and return, so
    importing an archetype left the fleet exactly as it was. It now posts the
    manifest to the orchestrator, which creates the skills, registers the MCP
    servers and -- with --create-instance -- provisions a bot from it.
    """
    from agentfleet.hub import ArchetypeManifest

    try:
        manifest = ArchetypeManifest.load(args.file)
    except Exception as e:
        print(f"❌ Import failed: {e}")
        return

    print(f"📦 {manifest.name} ({manifest.id})")
    print(f"   Category: {manifest.category} · Hardware: {manifest.recommended_tier}")
    print(f"   Tools: {', '.join(manifest.preinstalled_tools) or 'none'}")
    print(f"   Recorded skills: {len(manifest.recorded_skills)}")
    print(f"   MCP servers: {len(manifest.mcp_servers)}")

    if args.dry_run:
        print("\n(--dry-run: nothing was installed)")
        return

    client = _get_client(args)
    try:
        res = client._post(
            "/api/archetypes/import",
            {
                "manifest": manifest.to_dict(),
                "overwrite": bool(args.overwrite),
                "create_instance": bool(args.create_instance),
                "instance_name": args.instance_name or "",
            },
        )
    except Exception as e:
        print(f"❌ Import failed: {e}")
        return

    print()
    created = res.get("skills_created") or []
    skipped = res.get("skills_skipped") or []
    if created:
        print(f"✅ Skills created: {', '.join(created)}")
    if skipped:
        # Named rather than counted: a skill that was skipped because it already
        # exists is the one the operator most needs to know about.
        print(f"⏭️  Skills skipped (already present; use --overwrite): {', '.join(skipped)}")
    if res.get("mcp_registered"):
        print(f"✅ MCP servers registered: {', '.join(res['mcp_registered'])}")
    for failure in res.get("mcp_failed") or []:
        print(f"⚠️  MCP server not registered: {failure}")
    for secret in res.get("needs_secrets") or []:
        print(f"🔑 Needs a credential before it will work: {secret}")

    if res.get("instance_id"):
        print(f"✅ Bot provisioned: {res['instance_name']} ({res['instance_id']}) "
              f"— {res.get('instance_status')}")
    elif res.get("instance_status"):
        print(f"⚠️  Bot not provisioned: {res['instance_status']}")

    if not created and not res.get("mcp_registered") and not res.get("instance_id"):
        print("Nothing was installed: everything in the package was already present.")


# ---------------------------------------------------------------------- Main ---


def cmd_host(args: argparse.Namespace) -> None:
    """Let Oaf act on this machine: register it and execute its jobs here."""
    from agentfleet import host as _host

    client = _get_client(args)
    if not client.token:
        print("Not logged in. Run `fleetctl login` first (or set AGENTFLEET_TOKEN).", file=sys.stderr)
        raise SystemExit(2)
    import platform as _platform

    name = args.name or _platform.node() or "this-pc"
    try:
        _host.run(client, name=name, roots=args.root or [], auto_approve=args.yes, quiet=args.quiet)
    except KeyboardInterrupt:
        print()
        print("[host] disconnected")



# ------------------------------------------------------------- tickets/org ---

def _agent_ids(client: FleetClient) -> dict[str, str]:
    """Agent name (lower case) -> id, so commands take names."""
    return {b.name.lower(): b.id for b in client.list_bots()}


def _resolve_agent(client: FleetClient, name: Optional[str]) -> Optional[str]:
    if not name:
        return None
    ids = _agent_ids(client)
    if name.lower() in ids:
        return ids[name.lower()]
    if name in ids.values():
        return name
    raise SystemExit(f"no agent called {name!r}; the fleet has: {', '.join(sorted(ids)) or 'nobody'}")


def cmd_tickets(args: argparse.Namespace) -> None:
    client = _get_client(args)
    status = [s.strip() for s in (args.status or "").split(",") if s.strip()] or None
    assignee = _resolve_agent(client, args.assignee) if args.assignee else None
    rows = client.list_tickets(status=status, assignee=assignee, roots=args.roots, limit=args.limit)
    if not rows:
        print("No tickets. Ask the fleet for something in chat, or `fleetctl ticket new`.")
        return
    print(f"{'REF':<7} {'STATUS':<12} {'ASSIGNEE':<16} {'COST':>7}  TITLE")
    print("-" * 88)
    for t in rows:
        cost = f"${t.get('cost_usd', 0):.2f}" if t.get("cost_usd") else ""
        who = t.get("assignee_name") or ("-" if not t.get("assignee_id") else t["assignee_id"][:8])
        mark = {"review": "[review] ", "verify": "[verify] ", "unblock": "[unblock] "}.get(t.get("kind", ""), "")
        print(f"{t['ref']:<7} {t['status']:<12} {who:<16} {cost:>7}  {mark}{t['title'][:60]}")


def cmd_ticket(args: argparse.Namespace) -> None:
    client = _get_client(args)
    if args.action == "new":
        t = client.create_ticket(
            args.title, description=args.description or "",
            assignee_id=_resolve_agent(client, args.to),
            parent=args.parent, blocked_by=args.after or None,
            reviewer_id=_resolve_agent(client, args.review_by),
            verifier_id=_resolve_agent(client, args.verify_by),
            budget_usd=args.budget or 0)
        print(f"✅ {t['ref']} filed: {t['title']}" + (f" -> {t.get('assignee_name')}" if t.get("assignee_name") else ""))
    elif args.action == "show":
        d = client.ticket(args.ref)
        t = d["ticket"]
        why = " → ".join(f"{a['ref']} {a['title']}" for a in d.get("ancestry") or [])
        print(f"{t['ref']}  {t['title']}")
        print(f"status:   {t['status']}" + (f" ({t['blocked_reason']})" if t.get("blocked_reason") else ""))
        print(f"assignee: {t.get('assignee_name') or '-'}   reviewer: {t.get('reviewer_name') or '-'}   verifier: {t.get('verifier_name') or '-'}")
        if why:
            print(f"why:      {why}")
        if d.get("blockers"):
            print("waits on: " + ", ".join(f"{b['ref']} ({b['status']})" for b in d["blockers"]))
        if t.get("result"):
            print("\nresult:\n" + t["result"])
        for c in (d.get("comments") or [])[-15:]:
            print(f"\n[{c['kind']}] {c.get('author_name') or ''}: {c['body'][:600]}")
    elif args.action == "comment":
        client.comment_ticket(args.ref, args.text)
        print("✅ commented")
    elif args.action == "reopen":
        t = client.reopen_ticket(args.ref, args.text)
        print(f"✅ {t['ref']} reopened for {t.get('assignee_name') or 'its assignee'}")
    elif args.action == "move":
        t = client.update_ticket(args.ref, status=args.status)
        print(f"✅ {t['ref']} is now {t['status']}")


def cmd_org(args: argparse.Namespace) -> None:
    client = _get_client(args)
    chart = client.org()
    nodes = chart.get("nodes") or []
    kids: dict[str, list[dict[str, Any]]] = {}
    for n in nodes:
        kids.setdefault(n.get("reports_to") or "", []).append(n)
    labels = {k["kind"]: k["label"] for k in chart.get("kinds") or []}

    def line(n: dict[str, Any]) -> str:
        bits = [n["name"]]
        if n.get("title"):
            bits.append(f"({n['title']})")
        if n.get("kind") and n["kind"] != "desktop":
            bits.append(f"[{labels.get(n['kind'], n['kind'])}]")
        if n.get("hold") == "budget":
            bits.append("— held at budget")
        elif n.get("busy"):
            bits.append(f"— on {n.get('ticket_ref')}")
        elif not n.get("online"):
            bits.append("— offline")
        if n.get("budget_month_usd"):
            bits.append(f"${n.get('spend_month_usd', 0):.2f}/${n['budget_month_usd']:.0f}")
        return " ".join(bits)

    def walk(parent: str, prefix: str) -> None:
        children = sorted(kids.get(parent, []), key=lambda n: n["name"].lower())
        for i, n in enumerate(children):
            last = i == len(children) - 1
            print(prefix + ("└─ " if last else "├─ ") + line(n))
            walk(n["id"], prefix + ("   " if last else "│  "))

    print("You")
    walk("", "")

EXTERNAL_KINDS = ["claude_code", "codex", "hermes", "openclaw", "webhook"]
ON_DEVICE = ("claude_code", "codex", "hermes")


def _resolve_device(client: FleetClient, name: Optional[str]) -> dict[str, Any]:
    devs = client.devices()
    pcs = [d for d in devs if d.get("kind", "pc") == "pc"]
    if not name:
        if len(pcs) == 1:
            return pcs[0]
        raise SystemExit("which PC? --device <name>. Connected: "
                         + (", ".join(d["name"] for d in pcs) or "none -- run `fleetctl host --root <folder>` on it"))
    for d in pcs:
        if d["id"] == name or d["id"].startswith(name) or d["name"].lower() == name.lower():
            return d
    raise SystemExit(f"no PC called {name!r}; yours: {', '.join(d['name'] for d in pcs) or 'none'}")


def _under(path: str, roots: list[str]) -> bool:
    norm = lambda p: os.path.normcase(os.path.normpath(p))  # noqa: E731
    p = norm(path)
    return any(p == norm(r) or p.startswith(norm(r).rstrip("\\/") + os.sep) or p.startswith(norm(r).rstrip("\\/") + "/")
               for r in roots)


def cmd_devices(args: argparse.Namespace) -> None:
    client = _get_client(args)
    devs = client.devices()
    if not devs:
        print("No devices. Run `fleetctl host --root <folder>` on a PC to connect it.")
        return
    labels = {"claude_code": "Claude Code", "codex": "Codex", "hermes": "Hermes"}
    print(f"{'ID':<10} {'NAME':<20} {'ONLINE':<7} {'AGENT CLIS':<26} ROOTS")
    print("-" * 96)
    for d in devs:
        clis = ", ".join(labels.get(r, r) for r in d.get("runtimes") or []) or "-"
        print(f"{d['id'][:8]:<10} {d['name'][:19]:<20} {'yes' if d.get('online') else 'no':<7} {clis:<26} {'; '.join(d.get('roots') or [])}")


def _token_arg(args: argparse.Namespace) -> Optional[str]:
    if getattr(args, "token_env", None):
        v = os.environ.get(args.token_env)
        if not v:
            raise SystemExit(f"${args.token_env} is not set")
        return v
    if getattr(args, "ask_token", False):
        import getpass
        return getpass.getpass("token: ") or None
    return None


def cmd_agent(args: argparse.Namespace) -> None:
    client = _get_client(args)
    if args.action == "add":
        conn: dict[str, Any] = {}
        if args.kind in ON_DEVICE:
            dev = _resolve_device(client, args.device)
            if args.kind not in (dev.get("runtimes") or []):
                raise SystemExit(f"{dev['name']} has no {args.kind} CLI (it has: {', '.join(dev.get('runtimes') or []) or 'none'}). "
                                 "Install it there and restart `fleetctl host`.")
            roots = dev.get("roots") or []
            folder = args.folder or (roots[0] if roots else "")
            if not folder or not _under(folder, roots):
                raise SystemExit(f"the folder must be under one of {dev['name']}'s roots: {', '.join(roots) or 'none'}")
            conn = {"device_id": dev["id"], "cwd": folder, "autonomy": args.autonomy}
            if args.model:
                conn["model"] = args.model
        else:
            if not args.url:
                raise SystemExit(f"a {args.kind} agent needs --url")
            conn = {"url": args.url}
            if args.agent_id:
                conn["agent_id"] = args.agent_id
        if args.timeout:
            conn["timeout_sec"] = args.timeout
        fields: dict[str, Any] = {}
        for k in ("title", "capabilities", "trust"):
            if getattr(args, k, None):
                fields[k] = getattr(args, k)
        if args.reports_to:
            fields["reports_to"] = _resolve_agent(client, args.reports_to)
        if args.budget:
            fields["budget_month_usd"] = args.budget
        inst = client.add_external_agent(args.name, args.kind, conn, token=_token_arg(args), **fields)
        where = f" in {conn['cwd']}" if conn.get("cwd") else f" at {conn.get('url')}"
        print(f"✅ {inst.get('name', args.name)} added ({args.kind}{where}). Give it work: fleetctl ticket new \"...\" --to {args.name!r}")
    elif args.action == "set":
        iid = _resolve_agent(client, args.name)
        fields = {}
        for k in ("title", "capabilities", "trust"):
            if getattr(args, k, None) is not None:
                fields[k] = getattr(args, k)
        if args.reports_to is not None:
            fields["reports_to"] = "" if args.reports_to.lower() in ("you", "me", "none", "") else _resolve_agent(client, args.reports_to)
        if args.budget is not None:
            fields["budget_month_usd"] = args.budget
        if args.warn_pct is not None:
            fields["budget_warn_pct"] = args.warn_pct
        if args.model or args.autonomy or args.folder:
            cur = client._get(f"/api/instances/{iid}").get("connection") or {}
            if args.model:
                cur["model"] = args.model
            if args.autonomy:
                cur["autonomy"] = args.autonomy
            if args.folder:
                cur["cwd"] = args.folder
            fields["connection"] = cur
        tok = _token_arg(args)
        if tok:
            fields["token"] = tok
        if not fields:
            raise SystemExit("nothing to change; see `fleetctl agent set --help`")
        inst = client.set_profile(iid, **fields)
        print(f"✅ {inst.get('name', args.name)} updated: {', '.join(sorted(fields))}")


def cmd_fleet(args: argparse.Namespace) -> None:
    client = _get_client(args)
    if args.action == "export":
        tpl = client.export_fleet()
        text = json.dumps(tpl, indent=2)
        if args.output and args.output != "-":
            with open(args.output, "w", encoding="utf-8") as fh:
                fh.write(text + "\n")
            print(f"✅ {len(tpl.get('agents') or [])} agents written to {args.output} (no secrets)")
        else:
            print(text)
    elif args.action == "import":
        with open(args.file, encoding="utf-8") as fh:
            tpl = json.load(fh)
        res = client.import_fleet(tpl, dry_run=args.dry_run, rename=args.rename)
        verb = "would create" if args.dry_run else "created"
        print(f"{verb}: {', '.join(res.get('created') or []) or 'nobody'}")
        if res.get("renamed"):
            print("renamed: " + ", ".join(res["renamed"]))
        if res.get("skipped"):
            print("skipped (name taken; --rename to keep them): " + ", ".join(res["skipped"]))
        for n in res.get("notes") or []:
            print("note: " + n)


def main() -> None:
    parser = argparse.ArgumentParser(
        prog="fleetctl",
        description="OpenAgentFleet CLI — Manage hard-sandboxed autonomous OS agents & collaborative swarms",
    )
    parser.add_argument("--url", help="OpenAgentFleet orchestrator URL")
    parser.add_argument("--token", help="Authentication Bearer token")

    subparsers = parser.add_subparsers(dest="subcommand", required=True)

    # diagnostics / doctor
    p_diag = subparsers.add_parser("diagnostics", aliases=["doctor"], help="Run comprehensive platform health diagnostics")
    p_diag.set_defaults(func=cmd_diagnostics)

    # tickets and the org chart
    p_tks = subparsers.add_parser("tickets", help="List tickets: work with an owner and what it waits on")
    p_tks.add_argument("--status", help="Comma-separated: backlog,todo,in_progress,in_review,blocked,done,cancelled")
    p_tks.add_argument("--assignee", "-a", help="Agent name")
    p_tks.add_argument("--roots", action="store_true", help="Only top-level requests")
    p_tks.add_argument("--limit", type=int, default=100)
    p_tks.set_defaults(func=cmd_tickets)

    p_tk = subparsers.add_parser("ticket", help="File, show, comment on, reopen or move a ticket")
    tk_sub = p_tk.add_subparsers(dest="action", required=True)
    tk_new = tk_sub.add_parser("new", help="File a ticket")
    tk_new.add_argument("title")
    tk_new.add_argument("--to", help="Agent that takes it")
    tk_new.add_argument("--description", "-d")
    tk_new.add_argument("--parent", help="Ticket it exists for (T-12)")
    tk_new.add_argument("--after", action="append", help="Ticket it waits on (repeatable)")
    tk_new.add_argument("--review-by", help="Agent that reviews it when it finishes")
    tk_new.add_argument("--verify-by", help="Agent that verifies its subtree when it stops")
    tk_new.add_argument("--budget", type=float, help="Spend ceiling for this ticket, in dollars")
    tk_show = tk_sub.add_parser("show", help="Show a ticket")
    tk_show.add_argument("ref")
    tk_c = tk_sub.add_parser("comment", help="Comment on a ticket")
    tk_c.add_argument("ref")
    tk_c.add_argument("text")
    tk_r = tk_sub.add_parser("reopen", help="Send finished work back with what is missing")
    tk_r.add_argument("ref")
    tk_r.add_argument("text")
    tk_m = tk_sub.add_parser("move", help="Change a ticket's status")
    tk_m.add_argument("ref")
    tk_m.add_argument("status", choices=["backlog", "todo", "blocked", "done", "cancelled"])
    p_tk.set_defaults(func=cmd_ticket)

    p_org = subparsers.add_parser("org", help="Show the org chart: who reports to whom and what each is doing")
    p_org.set_defaults(func=cmd_org)

    p_devs = subparsers.add_parser("devices", help="Your connected PCs and phones, and the agent CLIs each has")
    p_devs.set_defaults(func=cmd_devices)

    p_ag = subparsers.add_parser("agent", help="Add a Claude Code, Codex, Hermes, OpenClaw or webhook agent, or change an agent's place in the org")
    ag_sub = p_ag.add_subparsers(dest="action", required=True)
    ag_add = ag_sub.add_parser("add", help="Add an external agent beside the desktops")
    ag_add.add_argument("name")
    ag_add.add_argument("--kind", "-k", required=True, choices=EXTERNAL_KINDS)
    ag_add.add_argument("--device", help="PC it runs on (claude_code, codex, hermes). Default: your only PC")
    ag_add.add_argument("--folder", help="Folder it works in, under one of the PC's roots. Default: the first root")
    ag_add.add_argument("--model", help="Model for the CLI (e.g. sonnet, haiku, gpt-5)")
    ag_add.add_argument("--autonomy", choices=["edits", "full"], default="edits",
                        help="edits: change files, no commands; full: anything inside its folder")
    ag_add.add_argument("--url", help="Gateway (ws:// or wss://) for openclaw, endpoint for webhook")
    ag_add.add_argument("--agent-id", help="OpenClaw agent id")
    ag_add.add_argument("--token-env", help="Read the gateway or webhook token from this environment variable")
    ag_add.add_argument("--ask-token", action="store_true", help="Prompt for the token (not echoed)")
    ag_add.add_argument("--timeout", type=int, help="Seconds one run may take")
    for p in (ag_add,):
        p.add_argument("--title")
        p.add_argument("--reports-to", help="Agent it reports to (default: you)")
        p.add_argument("--capabilities", help="When it is useful, in a line colleagues read")
        p.add_argument("--trust", choices=["standard", "low"])
        p.add_argument("--budget", type=float, help="Monthly spend ceiling in dollars (admin)")
    ag_set = ag_sub.add_parser("set", help="Change an agent's title, manager, capabilities, trust, budget or connection")
    ag_set.add_argument("name")
    ag_set.add_argument("--title")
    ag_set.add_argument("--reports-to", help="Agent it reports to, or 'you'")
    ag_set.add_argument("--capabilities")
    ag_set.add_argument("--trust", choices=["standard", "low"])
    ag_set.add_argument("--budget", type=float, help="Monthly spend ceiling in dollars; 0 for none (admin)")
    ag_set.add_argument("--warn-pct", type=int, help="Warn at this percentage of the budget (admin)")
    ag_set.add_argument("--model")
    ag_set.add_argument("--autonomy", choices=["edits", "full"])
    ag_set.add_argument("--folder")
    ag_set.add_argument("--token-env")
    ag_set.add_argument("--ask-token", action="store_true")
    p_ag.set_defaults(func=cmd_agent)

    p_fl = subparsers.add_parser("fleet", help="Export the fleet as a template, or import one")
    fl_sub = p_fl.add_subparsers(dest="action", required=True)
    fl_exp = fl_sub.add_parser("export", help="Agents, titles, reporting lines and budgets as JSON (no secrets)")
    fl_exp.add_argument("--output", "-o", help="File to write (default: stdout)")
    fl_imp = fl_sub.add_parser("import", help="Recreate a template here")
    fl_imp.add_argument("file")
    fl_imp.add_argument("--dry-run", action="store_true", help="Say what would happen, create nothing")
    fl_imp.add_argument("--rename", action="store_true", help="Rename agents whose name is taken instead of skipping them")
    p_fl.set_defaults(func=cmd_fleet)

    # host: let Oaf act on this machine
    p_host = subparsers.add_parser("host", help="Connect this PC so Oaf and your Claude Code, Codex or Hermes agents can work in folders you choose")
    p_host.add_argument("--root", "-r", action="append", help="Folder to expose (repeatable). Default: the current folder")
    p_host.add_argument("--name", "-n", help="Device name shown in the console (default: this computer's name)")
    p_host.add_argument("--yes", "-y", action="store_true", help="Run shell commands and writes without asking here first")
    p_host.add_argument("--quiet", "-q", action="store_true", help="Do not print each job")
    p_host.set_defaults(func=cmd_host)

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
    p_hub_exp = hub_subs.add_parser(
        "export", help="Export an archetype, its skills and its MCP servers to a package"
    )
    p_hub_exp.add_argument("archetype", help="Archetype ID")
    p_hub_exp.add_argument(
        "-o", "--output", help="Output file path (default: <id>.agentfleet.yaml; .json also works)"
    )
    p_hub_exp.add_argument(
        "--skills",
        help="Comma-separated skill ids or names to include (default: all of them)",
    )
    p_hub_exp.set_defaults(func=cmd_hub_export)

    p_hub_imp = hub_subs.add_parser(
        "import", help="Install an archetype package onto this fleet"
    )
    p_hub_imp.add_argument("file", help="Path to a .agentfleet.yaml or .agentfleet.json package")
    p_hub_imp.add_argument(
        "--overwrite",
        action="store_true",
        help="Replace skills that already exist (off by default, so an import "
        "cannot quietly overwrite a skill this fleet has been refining)",
    )
    p_hub_imp.add_argument(
        "--create-instance",
        action="store_true",
        help="Also provision a bot from the manifest",
    )
    p_hub_imp.add_argument("--instance-name", help="Name for the provisioned bot")
    p_hub_imp.add_argument(
        "--dry-run", action="store_true", help="Show what the package contains and install nothing"
    )
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

    # models & fallback chain
    p_models = subparsers.add_parser("models", help="Manage AI engines and tiered fallback order")
    mod_subs = p_models.add_subparsers(dest="models_action", required=True)
    p_mod_list = mod_subs.add_parser("list", help="List engines in fallback priority order")
    p_mod_list.set_defaults(func=cmd_models_list)
    p_mod_reorder = mod_subs.add_parser("reorder", help="Reorder fallback chain (Tier 1 -> Tier 2 -> ...)")
    p_mod_reorder.add_argument("ids", nargs="+", help="Provider IDs in desired priority order")
    p_mod_reorder.set_defaults(func=cmd_models_reorder)
    p_mod_probe = mod_subs.add_parser("probe", help="Test connectivity of an AI engine")
    p_mod_probe.add_argument("id", help="Provider ID")
    p_mod_probe.set_defaults(func=cmd_models_probe)

    args = parser.parse_args()
    if hasattr(args, "func"):
        args.func(args)
    else:
        parser.print_help()


if __name__ == "__main__":
    main()
