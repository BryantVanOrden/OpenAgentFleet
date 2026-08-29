# Agent philosophy

How AgentFleet divides the work of driving a desktop, and why. This document
separates what the code does today from what it is designed to grow into, and
labels which is which. Everything in "What ships" is traceable to a named file.

## The problem

A computer-use agent has to do several unrelated jobs. It has to look at a
screenshot and decide where to click. It has to reason about a failure it cannot
see. It has to write and run code. It has to summarise a long conversation so the
next turn fits in a context window. It has to take a recorded demonstration and
turn it into something reusable.

These want different models. Looking at a screen wants a fast vision model, and
wants it every turn, because there is one screenshot per step and screenshots are
the expensive part of the bill. Reasoning about a trace wants a model that
thinks, and wants it rarely. Summarising wants whatever is cheapest that can
read. Forcing one model to do all of it means paying frontier vision prices for
text summarisation, or asking a vision model to do deduction it is bad at.

The response is not a fixed two-model architecture. It is to let the operator say
which model does which job.

---

## What ships

### Role-based model routing

`backend/internal/connectors/roles.go`.

There are five roles: `vision`, `reasoning`, `chat`, `summarize`, `refine`
(`backend/pkg/protocol/types.go`). A **model combination** is a named mapping
from roles to providers — "these eyes, that brain". A bot's model chain is an
ordered list whose entries are each either a plain provider or a combination.
When a request is made for a role, the chain is expanded: a plain provider
answers for every role, a combination answers with whichever provider it assigns
to that role.

That is what makes "these two models, and if neither answers, that one"
expressible — combinations and single providers sit in the same list, so a
fallback chain and a role split are the same mechanism.

Where the roles are actually asked for:

| Role | Asked for by | For |
| :--- | :--- | :--- |
| `vision` | `internal/agent/runner.go` | The perceive-and-act turn: screenshot in, one action out. |
| `chat` | `internal/httpapi/chat.go`, `peer_responder.go` | Talking to the operator, and answering another agent. |
| `summarize` | `internal/httpapi/conversations.go` | Compacting a conversation that has grown too long. |
| `refine` | `internal/agent/refine.go` | Turning an execution trace into a better skill. |
| `reasoning` | — | No direct call site. It exists as a fallback target: a request for `summarize` or `refine` falls back to `reasoning` before it falls back to `chat`. |

The fallback order per role is in `roleFallback`. Two decisions in it are worth
stating. A combination that does not assign the role being asked for falls back
to another of its own models rather than being skipped, because the operator
picked those models and those are the ones that should be used. And `vision`
falls back to nothing: a request carrying a screenshot is filtered against the
provider's declared vision support downstream, so a text model reached by
fallback is dropped there rather than being asked to click blind.

Unknown entries in a chain are dropped rather than raising an error, so deleting
a provider or a combination does not break every bot that once referenced it.

### Grounding a click

The agent gets three things per turn, not one.

- **The frame**, as WebP, with **Set-of-Marks** badges drawn over interactive
  elements (`sandbox/agentd/som.py`). The model can answer "click [7]" rather
  than guessing a pixel.
- **The accessibility tree**, from AT-SPI (`sandbox/agentd/a11y.py`), flattened
  to individual widgets rather than top-level windows — which is what
  Set-of-Marks needs to badge anything useful.
- **The history** of what it has already done.

**Coordinate-space calibration** (`backend/internal/agent/coordspace.go`) exists
because vision models disagree about what a coordinate means and will not tell
you which convention they use. Measured on this project's hardware, `qwen2.5-vl`
answers in image pixels and `qwen3.5` answers in a 0–1000 range on both axes, and
prompting does not move either: the reply was byte-identical whether the prompt
said nothing, stated the pixel range, or included a worked example. So with
`AGENT_COORD_SPACE=auto` the agent shows the model one frame whose layout it
cannot know in advance, reads the convention off the answer, and caches it.

### A stateful Python REPL, not a stateless shell

`sandbox/agentd/repl.py`. Variables, functions, open handles and browser
sessions defined in one step survive into the next, and `a11y`, `capture` and a
curated `inject` facade are pre-bound. A workflow that would be forty GUI clicks
can be a few lines of Python instead.

The `python` action is gated by `ALLOW_SHELL` in `sandbox/agentd/main.py`, along
with `mount_tool` and `call_tool` — the REPL is arbitrary code execution and is
treated as such.

### Escalating instead of flailing

`backend/internal/agent/runner.go` hashes each frame. If the screen has not
changed across `StallThreshold` consecutive actions, the agent is clicking into
the void, and burning the remaining step budget will not help. It raises a
`stalled` alert, packages the state, and waits for the operator, who can reply
and let it continue.

The model can also escalate deliberately with the `ask_human` action, which the
system prompt tells it to use for CAPTCHAs, multi-factor prompts, payment
confirmations — and for any instruction arriving from a web page that claims to
be from the operator.

### Refining skills from execution traces

`backend/internal/agent/refine.go`. A completed run's step records are fed to the
`refine` role along with the original `SKILL.md`, and the model is asked to
replace fragile coordinate clicks with accessible labels, drop redundant waits,
and parameterise hard-coded inputs. `SynthesizeSkill` does the same for a run
with no skill attached, extracting one from scratch.

Be precise about when this fires. It runs automatically **on success only**, in
`Runner.succeed`, and only when the task has `AutoRefine` set or is attached to a
skill. There is no automatic analysis of a failed run. Refinement of a specific
skill or task can be triggered by hand through `POST /api/skills/{id}/refine` and
`POST /api/tasks/{id}/synthesize-skill`.

One implementation note that cost real time: refinement sets `DisableThinking`. A
reasoning model given a JSON-only request spends its budget on a hidden thinking
pass and has nothing left to emit, so the request comes back empty and the chain
falls through to whatever is next. Here the structured answer *is* the reasoning,
so the hidden pass is pure waste.

### Provider chains and sign-in

A bot has an ordered chain and a request walks it until something answers, so a
rate limit or a timeout on the first provider moves the turn to the second
without failing the task.

Providers are `openai`, `anthropic`, `gemini`, `antigravity`, `ollama`, and
`openai-compatible` for vLLM, LocalAI, LiteLLM and similar. Credentials are
either an API key or an OAuth 2.0 authorization-code sign-in with PKCE
(`backend/internal/httpapi/provider_oauth_code.go`), where the app shows the
provider's own consent page and the server does the exchange, so the app never
holds a secret or a token.

---

## Not implemented

These have been described in earlier versions of this document as though they
shipped. They do not. They are recorded here as intent, and as a correction.

- **A separate "DeepSeek harness".** There is no second agent loop, no separate
  reasoning process, and no DeepSeek-specific code path. DeepSeek appears in the
  tree exactly twice: as `deepseek-r1:14b` in the built-in Ollama model
  catalogue, and in a comment listing models that emit reasoning in a thinking
  block. You can point the `reasoning` or `refine` role of a combination at a
  DeepSeek model, and that is the whole of the integration — it is the same
  mechanism as pointing it at anything else.
- **Automatic post-failure trajectory autopsy.** Refinement runs on success. A
  failed run is not analysed, and nothing is deduced from it. Making failure the
  more interesting input is the obvious next step and it has not been taken.
- **Any cost multiple.** An earlier draft claimed a 10× reduction in operational
  cost. Nothing in this project has measured that. Splitting roles so that a
  local model handles summarisation instead of a frontier model plainly costs
  less than not doing so, and the size of the difference depends entirely on
  which models you pick and what you run. No figure is claimed.
- **Driving a third-party subscription.** An earlier draft described a fallback
  tier "powered by user's Antigravity subscription". There is no such thing.
  `antigravity` is a provider like any other: it needs a base URL and a
  credential, and it talks to a Gemini-compatible API endpoint. Neither an
  Antigravity subscription nor a Claude subscription can be used to drive this
  application; consumer subscriptions do not expose an API for third-party
  clients, and no amount of sign-in plumbing changes that. If you want a model
  here, you need API access to it.
- **Cross-checking vision against the accessibility tree.** The agent is given
  both a badged frame and an AT-SPI tree, and Set-of-Marks is built from the
  tree — but nothing compares the model's visual answer against accessibility
  metadata and probes when they disagree. The two are inputs to the same prompt,
  not a verification loop.
- **VM-level isolation.** The `developer-heavy` tier is described as the one most
  likely to run untrusted build scripts and is still a container. The QEMU driver
  is on the roadmap. See [SECURITY.md](SECURITY.md).

---

## The principle underneath

Where a design decision is not obvious, the reasoning belongs next to the code
that implements it, and the trade belongs in the docs. Several of the comments
quoted above — why `vision` has no fallback, why `no-new-privileges` is not set,
why thinking is disabled for refinement — exist because the alternative was
rediscovering the same problem a third time.

The corollary is this document's rule: if it is not in the source, it does not go
in the prose.
