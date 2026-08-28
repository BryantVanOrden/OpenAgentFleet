# Skills, Demonstration Recording & Continual Refinement

AgentFleet combines **Human Demonstration Recording** with **AI Continual Self-Improvement** to produce robust, self-healing computer-use workflows.

---

## 1. The Learn-by-Demonstration Paradigm

Hardcoding coordinate macros (`click 412, 108`) is notoriously brittle:
* Windows move or resize.
* Operating system themes and DPI scaling change.
* Browser layouts shift with new versions.

AgentFleet solves this by recording **semantic interactions**:
1. You perform the task once manually on the desktop.
2. The recorder captures raw mouse/keyboard events **simultaneously with the AT-SPI accessibility element** underneath each interaction.
3. The compiler lifts the trace into a structured `SKILL.md` — instructing the model to *"Click the button labelled 'Build'"*, not click a fixed pixel coordinate.

---

## 2. Event Capture & Compilation

### Capture Pipeline (`sandbox/agentd/recorder.py`)
While recording is active, `agentd` monitors:
* **X RECORD extension**: Key presses, mouse button clicks, double-clicks, drags, and scrolls.
* **AT-SPI D-Bus hierarchy**: Extracts the `role` (e.g. `push button`, `menu item`, `text entry`) and accessible `label` of the target widget at the exact millisecond of the click.

### Semantic Compilation (`backend/internal/recorder/compile.go`)
Raw input traces are full of noise (mouse jitter, key pauses, redundant focus events). `recorder.Compile` processes the raw trace:
* **Typing Aggregation**: Individual printable keystrokes are merged into single `type` steps. Pauses longer than 1.2s create separate field entries.
* **Gesture Lifting**: Clicks within 400ms and 6px are merged into `double_click`.
* **Focus Normalization**: Redundant window switches and temporary focus changes are eliminated.
* **Label Preservation**: Every step retains its accessible `role` and `label`, with screen coordinates stored only as a secondary fallback.

---

## 3. Rendered `SKILL.md`

The compiled skill is rendered into markdown that reads as instructions to a colleague:

```markdown
# Task: Build and Test Project

1. Focus window: "Terminal"
2. Click element: Role="push button", Label="Build Project"
3. Wait until the screen shows "Build Succeeded" (timeout 180s)
4. Type "npm test" into "Terminal"
5. Press Return
6. Assert: file:~/work/dist/app.bundle.js:1024

The recording is a guide, not a script: labels and layout may have moved.
Verify each outcome on screen before moving on.
```

---

## 4. Continual Harness & AI Self-Refinement (Prime Agent Integration)

Static procedures can decay as software updates. Inspired by Prime Agent's continual learning harness, AgentFleet includes an **Autonomous Refinement Engine** ([`backend/internal/agent/refine.go`](file:///c:/Users/borden/Documents/Code/Apps/AgentFleet/backend/internal/agent/refine.go)).

```
┌────────────────────────┐
│  Task Execution Trace  │  (Screenshots, actions, outcomes, retries)
└───────────┬────────────┘
            │
            ▼
┌────────────────────────┐
│  AI Refinement Loop    │  (Identifies fragile clicks, dead ends, layout drift)
└───────────┬────────────┘
            │
            ▼
┌────────────────────────┐
│  Updated SKILL.md v2   │  (Self-healed selectors, optimized waits, assertions)
└────────────────────────┘
```

### 1. Post-Task Self-Healing (`RefineSkill`)
When a task completes:
1. The orchestrator inspects the step history.
2. If the agent had to retry an action, fall back to coordinates, or discover a renamed button, the Refinement Engine asks the model to update the `Skill` definition.
3. Fragile coordinate clicks are replaced with newly verified AT-SPI role/label combinations.
4. The skill version is incremented (`v1` → `v2`) with descriptive **Continual Refinement Notes** logged in the database.

### 2. Autonomous Skill Synthesis (`SynthesizeSkill`)
If an operator launches an agent with a natural-language goal and **no prior demonstration**, the agent solves the task from scratch. Upon success:
* The orchestrator can automatically synthesize a brand new reusable `SKILL.md`.
* Discovers input parameters, creates semantic steps, and registers the skill in the catalog.

---

## 5. Timeline Editor & Parameterization

In the React Admin Console (**Skills** page):
* **Parameterization**: Mark typed strings as template variables (e.g. `{{branch}}`, `{{username}}`). When launching a task, operators pass runtime parameter values.
* **Step Reordering & Pruning**: Operators can delete missteps, adjust timeout bounds, or insert custom `assert` conditions.
* **"⚡ AI Refine" Button**: Trigger immediate AI refinement on any skill against its latest execution history.
