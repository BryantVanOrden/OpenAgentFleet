# The Bicameral Agent: Philosophy of the Prime & DeepSeek Harness

> *"Perception without deep reasoning is brittle; reasoning without grounded perception is blind."*

---

## 1. Executive Summary

AgentFleet is engineered around a **bicameral cognitive architecture** that combines a **Frontier Multimodal Executive ("Prime")** with a **Deep Local Reasoning Engine ("DeepSeek Harness")**. 

In conventional computer-use frameworks, a single cloud model is tasked with everything: perceiving high-resolution desktop frames, guessing UI pixel coordinates, synthesizing complex Python scripts, analyzing execution traces, and performing post-task playbooks. This naive single-model paradigm suffers from severe flaws:
1. **Runaway Token Costs**: Feeding multi-megabyte screenshots and heavy code iterations into \$15/M token cloud models causes economic exhaustion.
2. **Perceptual Fragility**: LLMs hallucinate coordinates and fail when buttons shift by 5 pixels.
3. **Shallow Reasoning**: Fast vision models lack the deep mathematical and programmatic chain-of-thought (CoT) required to debug complex multi-step terminal or browser failures.

AgentFleet resolves this by splitting the cognitive workload into **System 1 (Perception & Motor Action)** and **System 2 (Deep Symbolic Reasoning & Continual Self-Healing)**.

---

## 2. The Bicameral Mind: Prime vs. DeepSeek

```
                      ┌──────────────────────────────────────────────┐
                      │              THE OPERATOR / GOAL             │
                      └──────────────────────┬───────────────────────┘
                                             │
                                             ▼
                      ┌──────────────────────────────────────────────┐
                      │         SYSTEM 1: PRIME VISION CORE          │
                      │  (Claude 3.7 Sonnet / Antigravity Gemini 3.7)│
                      │                                              │
                      │  • High-Res Screen Perception (WebP / dhash) │
                      │  • Visual Coordinate Grounding (Set-of-Marks)│
                      │  • AT-SPI Accessibility Tree Spatial Parsing │
                      │  • Real-Time Desktop Interaction (Click/Type)│
                      └──────────────┬───────────────────────────────┘
                                     │
                    ┌────────────────┴────────────────┐
                    │                                 │
     [Direct UI Action]             [Complex Logic / Code / Error / Playbook]
                    │                                 │
                    ▼                                 ▼
       ┌────────────────────────┐      ┌──────────────────────────────┐
       │   XFCE DESKTOP SANDBOX │      │  SYSTEM 2: DEEPSEEK HARNESS  │
       │   agentd / X11 Display │      │   (DeepSeek R1 / V3 / Local) │
       └────────────────────────┘      │                              │
                    │                  │  • Stateful Python REPL Gen  │
                    │ [Observation]    │  • Symbolic & Math Deduction │
                    └─────────────────►│  • Trajectory Autopsy & Root │
                                       │  • SKILL.md Self-Refinement  │
                                       └──────────────┬───────────────┘
                                                      │
                                                      ▼
                                       ┌──────────────────────────────┐
                                       │   PERSISTENT HARNESS MEMORY  │
                                       │  Self-Healing v1 ➔ v2 Playbook│
                                       └──────────────────────────────┘
```

### 🧠 System 1: The Prime Executive (Perception & Motor Action)
* **Engines**: Claude 3.7 Sonnet (Hybrid Thinking + Computer Use), Google Antigravity (Gemini 3.7 Flash), GPT-4o.
* **Role**: The eyes and hands of the agent.
* **Specialization**:
  - Ingesting high-resolution desktop frames, active window hierarchies, and AT-SPI accessibility trees.
  - Translating visual intent into pixel-accurate coordinates using Set-of-Marks and screen scaling normalization.
  - Making rapid, real-time navigation decisions (e.g. clicking buttons, filling input fields, switching workspaces).
  - Recognizing when visual state has stalled or when human judgment (CAPTCHAs, 2FA, policy approvals) is mandatory.

### 🔬 System 2: The DeepSeek Reasoning Harness (Deduction & Evolution)
* **Engines**: DeepSeek R1, DeepSeek V3, Local High-Parameter Reasoning Models (Ollama `deepseek-r1:14b/70b`).
* **Role**: The analytical neocortex, code engine, and continual learning harness.
* **Specialization**:
  - **In-Sandbox Python REPL Execution**: Writing sophisticated, multi-line data extraction, AST parsing, and file manipulation scripts that execute directly inside the `agentd` persistent Python runtime.
  - **Trajectory Autopsy**: Analyzing step-by-step audit logs after an execution failure, inspecting terminal stderr, and deducing alternative strategies.
  - **Continual Playbook Compilation**: Ingesting raw human demonstration logs and converting brittle coordinate clicks into resilient, semantic `SKILL.md` procedures that survive UI layout changes.

---

## 3. Core Philosophical Pillars

### Pillar I: Asymmetric Cost Architecture
Visual tokens are expensive; local reasoning is free.
* Feeding a 1920×1080 screenshot into a frontier cloud API consumes hundreds of visual tokens per turn.
* The DeepSeek Harness offloads data processing, regex filtering, code compilation, and log auditing to local GPUs/CPUs.
* **Outcome**: A 10× reduction in operational cost without sacrificing multimodal precision.

### Pillar II: Dual-Layer Spatial Grounding
An agent must never click on a guess.
1. **Layer A (Visual)**: Prime identifies elements by geometry, color, and visual labels.
2. **Layer B (Semantic)**: The Harness cross-references the AT-SPI accessibility tree (`role`, `accessible_name`, `states`, `bounding_box`).
3. If visual cues and accessibility metadata disagree, the Harness triggers an immediate verification probe rather than clicking blind.

### Pillar III: Stateful REPL Substrate over Ephemeral Shells
Computer-use agents fail when they treat bash as a stateless black box.
* AgentFleet embeds a persistent, stateful Python REPL inside `agentd`.
* DeepSeek maintains variables, open database handles, browser sessions, and AT-SPI bus proxies across turns.
* Complex workflows are executed programmatically in Python rather than through 40 individual GUI clicks.

### Pillar IV: Continual Self-Refinement (`v1` ➔ `v2` Playbooks)
Human demonstration provides the initial seed; machine refinement makes it unbreakable.
* **Recording Phase**: A human demonstrates a procedure via the HUD recorder. Prime records raw clicks and keystrokes (`v1`).
* **Refinement Phase**: The DeepSeek Harness inspects the execution trail, eliminates redundant steps, replaces hardcoded screen coordinates with accessible selectors, and writes a production-grade `v2` `SKILL.md`.

```
Human Demonstration (v1)
  │ (Raw mouse clicks & pixel coordinates)
  ▼
DeepSeek Harness Refinement
  │ • Eliminates jitter and redundant pauses
  │ • Replaces (x: 842, y: 312) with accessible selector: [role='push button', name='Deploy']
  │ • Generates automated pre/post assertions
  ▼
Hardened Autonomous Skill (v2)
```

### Pillar V: Zero-Hubris Escalation
Autonomous agents must know what they do not know.
* When visual hash repetition exceeds threshold, or when high-risk security barriers (MFA, CAPTCHA, payment confirmation) appear, the Prime-DeepSeek harness halts execution.
* The state is packaged into a time-sensitive push alert to the operator's mobile companion app, allowing one-tap resolution from anywhere.

---

## 4. Multi-Tier Fallback Symphony

The Prime-DeepSeek architecture is supported by a multi-tier fallback hierarchy that ensures zero-downtime execution:

| Tier | Role | Provider | Latency / Cost Profile |
| :--- | :--- | :--- | :--- |
| **Tier 1 (Primary)** | Visual Executive & Computer Use | Claude 3.7 Sonnet | Frontier multimodal reasoning; high grounding accuracy. |
| **Tier 2 (Failover #1)** | High-Speed Subscription Executive | Google Antigravity (Gemini 3.7 Flash) | High throughput; powered by user's Antigravity subscription. |
| **Tier 3 (Failover #2)** | Heavy Local Reasoning & Fallback | DeepSeek R1 / Local Ollama (Qwen 2.5 VL) | 100% air-gapped, zero cloud cost, local hardware execution. |

If Tier 1 encounters a rate limit (`429`), quota exhaustion, or network timeout:
1. The orchestrator automatically transfers the turn context to Tier 2.
2. The DeepSeek Harness ensures all REPL state and memory embeddings remain intact.
3. The task completes without human intervention or step failure.

---

## 5. Summary

The combination of **Prime (Visual Executive)** and **DeepSeek (Reasoning & Refinement Harness)** gives AgentFleet its distinct edge:
* **Eyes to see** the interface with pixel-perfect clarity.
* **Hands to act** with surgical precision across containerized desktops.
* **A mind to reason, code, and self-heal** continuously from experience.
