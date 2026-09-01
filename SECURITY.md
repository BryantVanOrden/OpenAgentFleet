# Security policy

AgentFleet runs autonomous agent loops against real desktops and holds a Docker
socket with root-equivalent authority, so a vulnerability here can reach the
host. Please report one privately.

---

## Do not file public issues or PRs for security vulnerabilities

> [!CAUTION]
> **NEVER submit public GitHub Issues, public Pull Requests, or public forum posts detailing an unpatched security vulnerability or sandbox breakout.**
>
> Publicly disclosing zero-day vulnerabilities puts users, servers, and data at immediate risk.

---

## How to report

If you discover a security vulnerability, sandbox escape, privilege escalation, authentication bypass, or credential exposure:

1. **Option A: GitHub Private Security Advisory (Recommended)**
   - Navigate to the **Security** tab of this repository: `https://github.com/BryantVanOrden/AgentFleet/security/advisories`
   - Click **"Report a vulnerability"** to open a private disclosure thread directly with the maintainers.

2. **Option B: Encrypted / Direct Maintainer Email**
   - Send an email to **Bryant VanOrden**:
     📧 **`supermanismebvo123@gmail.com`**
   - Include:
     - A clear description of the vulnerability and the attack vector.
     - A minimal proof of concept, or reproduction steps.
     - Affected versions, platforms or component layers (Go orchestrator, sandbox `agentd`, React admin console, Flutter companion app, Python SDK).
     - Any suggested remediation.

---

## Response times

- **Acknowledgment**: Within **48 hours** of receiving your report.
- **Triage & Reproduction**: Within **5 business days**.
- **Private Patch Development**: We will develop and verify the fix in a private security fork.
- **Coordinated Disclosure**: Once a patched release is published, we will publicly acknowledge and credit your responsible disclosure in the release notes.

---

## In scope

- **Orchestrator Backend (`backend/`)**: Authentication, JWT validation, cryptographic credential vault (`MASTER_KEY` / AES-256-GCM), egress policies, and API boundaries.
- **Hard-Sandboxed Daemon (`sandbox/agentd/`)**: Cgroup enforcement, AT-SPI accessibility parsing, command execution boundaries, REPL sandboxing.
- **Web Admin Console (`admin/`)** & **Flutter Companion App (`mobile/`)**: Session security, auth token persistence, XSS, CSRF.
- **Access control (`backend/pkg/protocol/rbac.go`, `backend/internal/httpapi/`)**: Organisation membership, per-bot permission grants, and route gating.
- **Python SDK & CLI (`sdk/python/`)**: Token transmission, credential handling, deserialization safety.

For the full architectural threat model — what the sandbox isolation is and is
not worth, how sudo is gated and what that trade costs, and the limits of the
egress policy — see [docs/SECURITY.md](docs/SECURITY.md).

The README's "Design limits, stated plainly" section is the authoritative list
of what each capability deliberately does not do. A documented limit behaving
as documented is a design discussion, not a vulnerability — but a limit the
docs *fail* to state, or a boundary that does not hold as described, is
exactly what this policy wants reported.
