# Console ↔ App feature parity plan

Goal: the React console and the Flutter app expose the same features.
Rule: where the two disagree about how a feature works, **the Flutter app's
way wins**. No backend changes required — every gap below already has API
routes in production use by the other client.

## React console gains (from Flutter)

| # | Feature | Flutter source of truth |
|---|---------|------------------------|
| R1 ✅ | Work catalog in Vault: folder browser (breadcrumbs, move/rename/delete), code editor w/ syntax highlight + versioning, mini-app player (sandboxed) | `vault_screen.dart`, `work_editor_screen.dart`, `mini_app_screen.dart` |
| R2 ✅ | Chat sessions per bot (list/pin/rename/delete), plan bubbles w/ Approve+Discard, three send modes (chat / plan / task-with-confirmation) | `chat_screen.dart`, `chat_sessions_sheet.dart` |
| R3 ✅ | Instance controls: per-bot voice+speed, persona editor, per-bot model chain, bot memory (list+forget), shell toggle, sudo grant/revoke w/ warning, per-bot access grants | `instance_screen.dart` control menu + sheets |
| R4 | Fleet comms as conversations (grouped threads, pin/rename/delete, compact, membership-aware composer) replacing the flat P2P feed | `comms_screen.dart`, `conversation_screen.dart`, `new_conversation_sheet.dart` |
| R5 | Model combinations CRUD (Hands/Brain + advanced Chat/Summarise/Refine) and provider OAuth sign-in (Google; sign-in/out, status chips) | `model_combo_sheet.dart`, `provider_signin_sheet.dart` |
| R6 | Admin: departments (orgs) + members + bots-in-org, API keys (create w/ show-once, revoke), user disable + set-password, `viewer` role | `admin/*.dart` |
| R7 | Host telemetry card (CPU/mem/disk/GPU bars); alerts Acknowledge sends an EMPTY reply (Flutter semantics); triggers show last-run + active badge | `host_usage_card.dart`, `alerts_screen.dart` |

## Flutter app gains (from React)

| # | Feature | React source of truth |
|---|---------|----------------------|
| F1 ✅ | Task step timeline (screenshots, action, thought, outcome, tokens) + assign-task with skill picker + sub-agent tree | `InstanceDetail.tsx` ActivityPane/sidebar |
| F2 ✅ | Swarms: list, launch (real-instance roster w/ roles), blackboard messages + composer, artifact approve/reject — reachable from Fleet app bar | `MissionControl.tsx` |
| F3 ✅ | Financials: KPI cards + recent turn telemetry | `Financials.tsx` |
| F4 | MCP hub: servers list/add/delete, discovered tools | `MCPHub.tsx` |
| F5 | Trigger authoring: create/delete webhooks (kinds, secret generate) and crons | `Triggers.tsx` |
| F6 | Pipeline authoring: stages, dependencies w/ 7 conditions, validation (cycles, regex), max-parallel; delete pipeline; runs view | `PipelineEditor.tsx`, `Pipelines.tsx` |
| F7 | Skills manager: list, edit steps/params, AI refine, SKILL.md view, delete | `Skills.tsx` |
| F8 | Provision advanced: hardware overrides + GPU + egress (block-local, allow-list); optional first goal (quick-launch collapse); login first-run bootstrap; credential refs admin; healthz figures | `Fleet.tsx`, `QuickLaunch.tsx`, `Login.tsx`, `Settings.tsx` |
| F9 | Archetype package export/import | `ArchetypePackages.tsx` |

## Conflicts resolved Flutter's way

- Chat: sessioned chats + plan approval (console's single-thread Ask/Run-as-task retired).
- Alerts: Acknowledge = empty reply that unblocks without instruction.
- Alerts view: always fetch all, split Waiting-on-you / History (no include-resolved toggle).
- Provision egress: off unless explicitly enabled (no silent default-on).
- Providers list: arrow-button reorder (both already agree).

Progress is tracked by checking items off this file.
