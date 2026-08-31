# AgentFleet v1.2.0

Seventy-two commits since v1.1.0.

v1.1.0 was about several of each thing — several departments, several models,
several bots in a row. This release is about what those bots actually produce.
They now have somewhere to put work, a way to hand it to each other, and a way
to run what a colleague built rather than only read it.

The full list is in [CHANGELOG.md](../CHANGELOG.md). This page covers what is
worth knowing before you upgrade.

---

## A shared work catalog

Agents could message each other and share credentials but had nowhere to put
the work itself. Whatever one produced lived in its container and died with it,
so a second bot asked to build on it had to be told what to rebuild.

They now publish **files**, **workspaces**, and **apps** — a self-contained
HTML document the Flutter app renders and runs. `publish_work` and `read_work`
are ordinary agent actions, scoped to the publishing bot's departments, so the
catalog is shared within a department rather than across the deployment.

Apps render in a sandboxed web view under a strict Content-Security-Policy:
`default-src 'none'`, no network of any kind, scripts inline only. An app that
tries to phone home does not get to. Script errors surface as plain text rather
than a blank rectangle.

## Agents that hand work to each other

Naming several agents in one message now produces a pipeline rather than three
bots doing the same job. Whoever is named first starts; everyone named after
them registers to wait and is handed the artefact when it exists. Stages run
design → build → test → review → build, and a reviewer's defects come back to
the builder as a concrete instruction — fix these, republish under the same
name — rather than something to interpret.

Name matching is fuzzy and order-sensitive, so "toolcheck", "Tool Check" and a
plausible misspelling all reach the same bot, and the order you wrote them in is
the order they work in.

Two failures this produced are worth naming, because both looked like the
agents being stupid:

- A job where everyone was asked to test and nobody to build used to sit
  silently while the agents' own replies — "I will test the thing" — read
  exactly like work starting. It now says so in the thread, once, naming who is
  stuck. It stays quiet while an agent that *was* asked is merely busy, because
  a busy agent's reply is queued behind its model call, not dropped.
- Stage keywords matched as substrings, so "markdown **preview** box" made the
  builder register as a reviewer, and the job correctly reported that nothing
  would ever reach it. Whole words only now.

## Testers can run what they test

This is the change most likely to alter what you see day to day.

An agent sent to try an app could read its source and nothing else. One spent
forty steps hunting for somewhere to run it — typing `http://localhost:8080/…`,
which nothing serves, then searching the web and landing on a real company that
happened to share the app's name. The review it was about to write would have
been about a stranger's website.

`read_work` now writes anything a browser can show into the sandbox, opens it,
and leads its answer with the path. Three smaller things had to be true for that
to work, and each had been quietly breaking runs:

- **The browser has to be alive.** A sandbox Firefox had been wedged for over an
  hour — "Firefox is already running, but is not responding" — and every agent
  sent to test something was driving a dead browser. No bot can fix that itself;
  they have no shell. One that does not answer within ten seconds is replaced.
- **Firefox must not open on its onboarding modal.** A fresh profile put
  "Welcome to Firefox" over the page under test; the agent clicked Continue, got
  a near-identical panel, and was judged to be clicking into the void.
- **Clicks must not time out.** `xdotool mousemove --sync` waits for a motion
  event, and moving the pointer somewhere it already is produces none — so it
  blocked for the full fifteen seconds. Clicking the same button twice was
  enough to trigger it.

## The shared work tab is a file system

The catalog had folders in the data and a flat list in the app. You could see
what was in a workspace but not put anything there, move anything out, rename
anything, or change a file without asking an agent to republish it.

It now walks folders one level at a time with a breadcrumb, and everything can
be created, renamed, moved, edited or deleted. Moving refuses to offer a folder
that sits inside the thing being moved, because the server refuses it too.

Runnable items keep their tap for "play" and put the rest behind a long press —
the menu says "Edit source", because a game is still a file.

The editor colours what it is showing: HTML, CSS, JavaScript, JSON, Dart, Go,
Python, shell, SQL, YAML and Markdown, picked from the file name first and the
content second, since agents publish `rollr` rather than `rollr.html`.

## Recorded demonstrations that can be followed

Recording a demonstration used to fail at the last step: stopping it returned
"unsupported Unicode escape sequence" and the whole trace was discarded after it
had been performed. The NUL in it was the Shift key, which also split typing in
two around itself; shifted punctuation was recorded unshifted, so a `file://`
URL came back with a semicolon in it; and keys that are not characters had no
name, rendering as "Press " with nothing after.

Steps also carry a picture now. A step was a coordinate and, where the
application exposed one, an accessible label — and Firefox exposes nothing, so a
browser demonstration compiled to `Click at 690,121 (no accessible label was
exposed)`. The recorder takes a small frame at each moment worth one, and the
compiler asks the vision model what is at the point that was touched:

```
2. Click element labelled "the address bar at the top of the browser"
```

The frames are kept beside the trace, so a person reviewing a skill can see what
the demonstration saw.

**Known limitation.** Replaying a skill gets started and then loses its place: in
testing, an agent followed the first steps, found the address bar by its
description, then clicked it repeatedly instead of moving on. Recording and
compiling are sound; following a skill step by step is not yet reliable.

## Administration separated from use

Model providers, tiered fallback and API-key management are administrator-only
and live in their own tab. Regular users get the fleet, not the wiring.
Administrators can create users, set passwords, move bots between departments —
a bot can belong to several — and revoke keys.

## Fixes worth calling out

- **API keys with `_` in the secret were rejected.** The parser split on every
  underscore, and base64url secrets contain them; roughly half of all issued
  keys did not work.
- **`instances.vnc_view_url` had no column**, so read-only auditor desktops
  never survived a restart. It failed closed, so this was never a way in.
- **Parked tasks were abandoned by restarts**, and since parked counts as busy,
  those agents never became available again.
- **Model capabilities are asked, not guessed.** Vision support was read out of
  the model's name; the default model in `.env.example` reports vision and was
  being offered as text-only. Discovery with a blank `base_url` also defaulted
  to localhost — nothing, inside a container — and quietly served a curated list
  containing models the machine has never had.
- **An app in the catalog cannot be replaced by something that is not one.** A
  tester published its report under the app's own name and the working app was
  gone.

---

## Upgrading

Migrations run at server start; there are 28, and they apply cleanly to an empty
database. Nothing in this release requires manual data migration.

One deployment change is worth applying by hand if you run your own compose
file: `db` and `minio` now carry `restart: unless-stopped`. Without it they are
the only services that do not come back after a reboot, and the API spends the
time restarting against a database that is not there.

Push notifications still require a Firebase project you create yourself — a
`google-services.json` for the app and a service-account JSON for the server.
Without them the app polls, which works but is not instant.
