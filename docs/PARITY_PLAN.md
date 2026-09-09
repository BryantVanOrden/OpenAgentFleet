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
| R4 ✅ | Fleet comms as conversations (grouped threads, pin/rename/delete, compact, membership-aware composer) replacing the flat P2P feed | `comms_screen.dart`, `conversation_screen.dart`, `new_conversation_sheet.dart` |
| R5 ✅ | Model combinations CRUD (Hands/Brain + advanced Chat/Summarise/Refine) and provider OAuth sign-in (Google; sign-in/out, status chips) | `model_combo_sheet.dart`, `provider_signin_sheet.dart` |
| R6 ✅ | Admin: departments (orgs) + members + bots-in-org, API keys (create w/ show-once, revoke), user disable + set-password, `viewer` role | `admin/*.dart` |
| R7 ✅ | Host telemetry card (CPU/mem/disk/GPU bars); alerts Acknowledge sends an EMPTY reply (Flutter semantics); triggers show last-run + active badge | `host_usage_card.dart`, `alerts_screen.dart` |

## Flutter app gains (from React)

| # | Feature | React source of truth |
|---|---------|----------------------|
| F1 ✅ | Task step timeline (screenshots, action, thought, outcome, tokens) + assign-task with skill picker + sub-agent tree | `InstanceDetail.tsx` ActivityPane/sidebar |
| F2 ✅ | Swarms: list, launch (real-instance roster w/ roles), blackboard messages + composer, artifact approve/reject — reachable from Fleet app bar | `MissionControl.tsx` |
| F3 ✅ | Financials: KPI cards + recent turn telemetry | `Financials.tsx` |
| F4 ✅ | MCP hub: servers list/add/delete, discovered tools | `MCPHub.tsx` |
| F5 ✅ | Trigger authoring: create/delete webhooks (kinds, secret generate) and crons | `Triggers.tsx` |
| F6 ✅ | Pipeline authoring: stages, dependencies w/ 7 conditions, validation (cycles, regex), max-parallel; delete pipeline; runs view | `PipelineEditor.tsx`, `Pipelines.tsx` |
| F7 ✅ | Skills manager: list, edit steps/params, AI refine, SKILL.md view, delete | `Skills.tsx` |
| F8 ✅ | Provision advanced: hardware overrides + GPU + egress (block-local, allow-list); optional first goal (quick-launch collapse); login first-run bootstrap; credential refs admin; healthz figures | `Fleet.tsx`, `QuickLaunch.tsx`, `Login.tsx`, `Settings.tsx` |
| F9 ✅ | Archetype package export/import | `ArchetypePackages.tsx` |

## Conflicts resolved Flutter's way

- Chat: sessioned chats + plan approval (console's single-thread Ask/Run-as-task retired).
- Alerts: Acknowledge = empty reply that unblocks without instruction.
- Alerts view: always fetch all, split Waiting-on-you / History (no include-resolved toggle).
- Provision egress: off unless explicitly enabled (no silent default-on).
- Providers list: arrow-button reorder (both already agree).

Progress is tracked by checking items off this file.

## Outcome

Parity reached on 2026-09-01. Both clients now expose the full feature set;
the phone app additionally owns voice I/O (dictation, read-aloud) as its
platform-native extra, and the console owns multi-column layouts. Defects
were not given parity: the console's hardcoded archetype dropdowns now read
the template catalogue, the phone's voice-speed reset now actually resets
(sends 0 rather than omitting the field), and the phone no longer offers
`viewer` as a platform role the backend refuses.

## Addendum — the fleet chat (2026-09-02)

Both clients now open on one chat with the whole fleet, and the swarm surfaces
(console Mission Control, app Swarms screen) are gone: missions run from the
chat via `/mission`, `/missions`, `/approve`, `/reject`. The slash-command
catalogue and executor are server-side (`GET /api/fleet/commands`,
`POST /api/fleet/command`), which is what keeps the two clients' verbs
identical by construction rather than by diligence. Parity items that fell
out of it, both sides: markdown in chat bubbles, `continued` task state and
the `↻ window N` chip for marathon runs, and the `progress` alert kind. The
phone's tab bar is Chat · Fleet · Vault · Alerts · Settings (· Admin); Pipelines
moved into the Fleet app bar, next to Skills and Fleet comms.

## Addendum — first-run setup card (2026-09-08)

Both clients read `GET /api/setup` with the fleet chat stream and show the same
card until `next` is `ready`: step counter, title, hint, one primary button
that runs the fleet command for the step (`/setup`, `/new fullstack_dev Scout`,
or focus the composer), an "I have an address" field for `/setup <url>`, and a
three-segment progress rail. Console: `admin/src/components/SetupCard.tsx`.
App: `mobile/lib/features/home/setup_card.dart`. The card is a front for chat
verbs, so there is no client-side setup logic to drift.

## Addendum — Oaf sessions, devices and attachments (2026-09-08)

The home screen of both clients is a picker over three kinds of conversation:
the fleet channel, sessions with Oaf, and bot-to-bot threads. Console: a rail
(`SessionRail.tsx`) with inline rename, pin and delete, `OafChat.tsx` for the
session (tool rows, attachments by picker/drop/paste, dictation and voice
mode, device/folder/model settings), `FleetChannel.tsx` for the channel. App:
a sheet behind the menu button (`sessions_sheet.dart`, long-press for rename,
pin, delete), `oaf_chat_screen.dart` (tool rows, photo attachments, mic and
voice mode), `session_settings_sheet.dart`. Both talk to the same
`/api/oaf/*` routes and render the same message kinds (`message`, `tool`).
Devices: the PC through `fleetctl host`; the phone registers itself from
Settings (`phone_device_card.dart`, `phone_device_service.dart`) for the
tools a phone can do. The console's per-agent Chat and Activity tabs wear the
same bubble and card styles as the session view.

## Addendum — Oaf sessions, devices, one-box agent chat (2026-09-08)

Both clients keep the same three kinds of conversation on the home tab: the
fleet channel, sessions with Oaf (`/api/oaf/sessions` — rename, pin, delete,
device + folder + model), and threads between bots. Console: a rail
(`admin/src/components/SessionRail.tsx`, `OafChat.tsx`); app: a sheet behind
the menu button (`mobile/lib/features/home/home_shell.dart`, `sessions_sheet.dart`,
`oaf_chat_screen.dart`, `session_settings_sheet.dart`). Tool calls render as
folded rows in both; attachments (drop/paste/picker on the console, photo picker
on the phone) render inline. Voice: dictation and a voice mode on both.
Devices: the console documents `fleetctl host`; the app registers the phone
itself from Settings (`phone_device_card.dart`, `core/device/phone_device_service.dart`).
The per-agent chat lost its Plan / Run buttons on both — one box, the agent
reads intent (`chat.go` `intentToAct`) — and recording is only offered in the
desktop view on both.
