# Security & Responsible Vulnerability Disclosure Policy 🛡️

AgentFleet takes security and sandboxing guarantees very seriously.

Because AgentFleet executes untrusted web workflows, sandboxed desktop applications, and autonomous agent loops, security vulnerabilities could expose environments if not handled responsibly.

---

## 🚨 CRITICAL: DO NOT File Public Issues or Public PRs for Security Vulnerabilities

> [!CAUTION]
> **NEVER submit public GitHub Issues, public Pull Requests, or public forum posts detailing an unpatched security vulnerability or sandbox breakout.**
>
> Publicly disclosing zero-day vulnerabilities puts users, servers, and data at immediate risk.

---

## 🔒 How to Responsibly Report a Security Vulnerability

If you discover a security vulnerability, sandbox escape, privilege escalation, authentication bypass, or credential exposure:

1. **Option A: GitHub Private Security Advisory (Recommended)**
   - Navigate to the **Security** tab of this repository: `https://github.com/BryantVanOrden/AgentFleet/security/advisories`
   - Click **"Report a vulnerability"** to open a private disclosure thread directly with the maintainers.

2. **Option B: Encrypted / Direct Maintainer Email**
   - Send an email to **Bryant VanOrden**:
     📧 **`supermanismebvo123@gmail.com`**
   - Include:
     - A clear description of the vulnerability and attack vector.
     - Minimal Proof of Concept (PoC) or reproduction steps.
     - Affected versions, platforms, or component layers (Go Orchestrator, Sandbox `agentd`, React Admin, Flutter Companion).
     - Any suggested remediations or patches.

---

## ⏱️ Response & Remediation SLA

- **Acknowledgment**: Within **48 hours** of receiving your report.
- **Triage & Reproduction**: Within **5 business days**.
- **Private Patch Development**: We will develop and verify the fix in a private security fork.
- **Coordinated Disclosure**: Once a patched release is published, we will publicly acknowledge and credit your responsible disclosure in the release notes.

---

## 🛡️ In-Scope Components

- **Orchestrator Backend (`backend/`)**: Authentication, JWT validation, cryptographic credential vault (`MASTER_KEY` / AES-256-GCM), egress policies, and API boundaries.
- **Hard-Sandboxed Daemon (`sandbox/agentd/`)**: Cgroup enforcement, AT-SPI accessibility parsing, command execution boundaries, REPL sandboxing.
- **Web Admin Console (`admin/`)** & **Flutter Companion App (`mobile/`)**: Session security, auth token persistence, XSS, CSRF.
- **Python SDK & CLI (`sdk/python/`)**: Token transmission, credential handling, deserialization safety.

For our full architectural threat model, see [docs/SECURITY.md](docs/SECURITY.md).
