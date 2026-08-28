# Roadmap

## Built

**Phase 1 — sandbox and remote view.** Ubuntu + XFCE + Xvfb + x11vnc + noVNC
image; Docker driver over the engine REST API; four hardware tiers with cgroup
enforcement; instance lifecycle (provision, start, stop, freeze, destroy) with
reconciliation against what the engine actually reports; authenticated desktop
proxy with WebSocket tunnelling.

**Phase 2 — connectors and the agent loop.** Ollama, OpenAI, Anthropic, Gemini
and any OpenAI-compatible gateway behind one interface, arranged as a
priority-ordered fallback chain with a penalty box for failing endpoints;
`agentd` observe/act daemon; the perceive → decide → act loop with a closed
action vocabulary, strict JSON parsing, stall detection on a perceptual hash, and
step-budget enforcement; full audit trail with per-step screenshots.

**Phase 3 — record and replay.** X RECORD input capture paired with AT-SPI
element context; compilation into semantic steps and a rendered `SKILL.md`;
timeline editor in the console for pruning steps and parameterising typed values.

**Phase 4 — companion app and alerts.** Flutter app with fleet dashboard, live
stats, embedded desktop stream, agent chat with a deliberate ask/run split,
and the resolution centre; FCM and APNs dispatch with time-sensitive delivery
for critical alerts; human-in-the-loop escalation that parks a task until someone
answers.

**Cross-cutting.** AES-256-GCM credential vault; RBAC with three roles; nftables
egress policy; artifact storage on filesystem or S3/MinIO; in-process event bus
with WebSocket fan-out.

## Next

### Phase 5 — heavy workloads and real isolation

The `developer-heavy` tier currently runs as a container with a GPU device
request. That is fine for compiling something you trust and wrong for anything
else.

- QEMU/KVM driver behind the existing `Driver` interface, with `vfio-pci` GPU
  passthrough.
- Copy-on-write base images so a 150 GB dev box provisions in seconds rather than
  minutes.
- Snapshot and restore, so a build environment can be rewound instead of rebuilt.

### Phase 6 — WebRTC streaming

noVNC over WebSocket is reliable and roughly 200–400 ms round trip. A
`selkies-gstreamer` pipeline gets that under 100 ms with hardware encoding, which
is the difference between "watchable" and "usable" for manual takeover on a
phone. The signalling port is already reserved in the image.

### Phase 7 — multi-node

The two things standing in the way, both deliberately narrow:

- Replace the in-process bus with Redis pub/sub (three methods).
- Give the fleet manager a scheduler that can place an instance on one of several
  hosts and a lease so two orchestrators do not both drive the same task.

### Phase 8 — better perception

- OCR fallback for windows that expose nothing to AT-SPI — canvas apps, games,
  video — so `wait_for` and `assert` are not blind there.
- Set-of-marks overlays: label interactive elements on the screenshot itself,
  which measurably improves grounding for smaller vision models.
- Frame diffing between steps, so the model is told what changed rather than
  having to work it out from two full screenshots.

### Phase 9 — operations

- Session video export (the frames are already stored; they need muxing).
- Cost accounting per task and per instance from the token counts already
  recorded.
- Scheduled and event-triggered runs.
- Skill versioning with a diff view, so an edit to a recording is reviewable.

## Deliberately not planned

- **A hosted multi-tenant service.** The security model assumes the operator owns
  the host. Multi-tenancy would need a different isolation story from the ground
  up, starting with never handing anything the Docker socket.
- **Fully unattended operation with no escalation path.** The `ask_human`
  mechanism is not a limitation to engineer away; it is the safety property.
