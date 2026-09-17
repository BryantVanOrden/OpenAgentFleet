# Security policy

OpenAgentFleet holds a Docker socket, drives real desktops and stores
credentials. A vulnerability here can reach the host. Report it privately.

## Report privately

Do not open a public issue, pull request or discussion for an unpatched
vulnerability, sandbox escape, privilege escalation, authentication bypass or
credential exposure.

Use **Report a vulnerability** under the repository's Security tab:
<https://github.com/BryantVanOrden/OpenAgentFleet/security/advisories/new>.
If that is not possible, email `supermanismebvo123@gmail.com`.

Include what the bug is, how to reproduce it (a minimal proof of concept is
ideal), which versions and components are affected, and any fix you have in
mind.

## What happens next

- Acknowledged within 48 hours.
- Triaged and reproduced within five business days.
- Fixed privately, released, then credited in the release notes if you want
  the credit.

## In scope

- `backend/`: authentication and JWT handling, the AES-256-GCM credential vault
  and `MASTER_KEY`, egress policies, RBAC and route gating, API boundaries.
- `sandbox/agentd/`: command execution boundaries, the Python REPL, cgroup and
  network isolation as documented.
- `admin/` and `mobile/`: session and token handling, XSS, CSRF.
- `sdk/python/`: token transmission, credential handling, deserialisation.

## What is not a vulnerability

The threat model in [docs/SECURITY.md](docs/SECURITY.md) and the README's
"Design limits, stated plainly" say what the isolation is and is not worth. A
documented limit behaving as documented is a design discussion. A limit the
docs fail to state, or a boundary that does not hold as described, is exactly
what this policy wants to hear about.
