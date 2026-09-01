# AgentFleet v1.3.0

The release that empties the "partly built" list — and relicenses the project
to MIT.

Every feature the README used to list as partly built is now built and tested.
What remains in that section (renamed *Design limits, stated plainly*) is a
single deliberate trade, not unfinished work.

## Open source, for real

- **MIT license**, aligned across the repository, the Python SDK's packaging
  metadata, the console's manifest, CONTRIBUTING's grant and the PR template.
  Use it, fork it, ship it — personally or commercially.

## Semantic memory ships by default

- A local embedding sidecar (`embed/`, model2vec potion-base-8M — ~30 MB,
  CPU-only, weights baked into the image) joins the compose stack as a default
  service. A fleet with no embedding-capable provider — Anthropic-only, or
  none configured yet — still gets semantic recall: *"sign in to the billing
  portal"* finds *"logged into the invoicing site with the shared credential"*
  with zero shared words. Configured providers that can embed are preferred
  for quality; `/api/memory/fleet` reports the live scheme.

## The MCP bridge speaks the whole protocol

- `resources/list`/`read` and `prompts/list`/`get` alongside tools, paginated,
  with optional capability groups handled per spec. Agents reach resources and
  prompts through `call_mcp`; the API serves them under `/api/mcp/`.
- Server notifications are acted on: `notifications/*/list_changed` re-fetches
  the catalogue automatically, on both stdio and Streamable HTTP.

## Swarms plan before they execute

- A swarm created with `plan_first` holds execution until every member has
  published a plan artifact — non-plan artifacts are refused during planning,
  the barrier lifts itself when the last plan lands, and each executor is
  handed the plans the team agreed. `POST /api/swarms/{id}/advance` is the
  operator override for a stuck member.

## Pipeline runs survive restarts

- Runs persist on every node transition and resume at boot from their last
  settled node: settled results and already-decided branches kept, mid-flight
  nodes re-dispatched, orphaned runs closed out instead of stuck at `running`.

## Cost telemetry prices honestly

- Model prices are fetched from OpenRouter's public catalogue at boot and
  daily (406 models on first fetch), used only on exact model-name matches,
  with the built-in table as the offline fallback (`PRICING_REFRESH=off` for
  air-gapped fleets). The financial summary reports its own pricing source and
  age. Cached prompt tokens are counted and discounted per vendor.

## Webhooks know their senders

- **HubSpot**: real v3 signature verification (and v1), replay-bounded, with
  summaries read from the actual event schema.
- **Salesforce**: outbound messages parsed as SOAP XML, authenticated against
  the expected OrganizationId, and acknowledged with the SOAP Ack — without
  which Salesforce retries the same mission for 24 hours.
- GitHub and Stripe were already verified properly; the generic `crm` kind
  remains for form backends, described as the heuristic it is.

## The README shows the product

- The hero is a live capture: one agent's own XFCE desktop streaming into the
  console, with the true caption — click the stream and you are driving.
  Companion-app screenshots sit alongside. All reproducible: `make
  screenshots`, `make hero-shot`, `make app-screenshots`.

## Fixed by looking

Building and photographing a live system, rather than mocking one, surfaced
and fixed along the way: Firefox's terms-of-use modal escaping the sandbox's
enterprise policy (`SkipTermsOfUse` was nested where Firefox ignores it), a
provisioning race against Docker's network attach, a CRLF checkout crash-loop
in the sandbox entrypoint on Windows-built images (now pinned by
`.gitattributes`), the app's Pipelines screen describing an engine that had
been replaced, and a boot race that left a healthy embedding sidecar unused
until the next restart.

Full details in the [changelog](../CHANGELOG.md).
