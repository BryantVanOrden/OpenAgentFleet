# Contributing to AgentFleet

Thank you for your interest in contributing to **AgentFleet**!

AgentFleet is built on the philosophy of **hard-sandboxed, auditable, and resilient autonomous OS agents**.

---

## 🛠️ Development Setup

### 1. Prerequisites
- **Docker & Docker Compose**
- **Go 1.23+** (Orchestrator Backend)
- **Node.js 20+ & npm** (React Admin Console)
- **Python 3.10+** (Sandbox `agentd` & Python SDK)
- **Flutter 3.24+** (Mobile Companion)

### 2. Local Environment
```bash
# 1. Clone your fork
git clone https://github.com/<your-username>/AgentFleet.git
cd AgentFleet

# 2. Spin up local development infrastructure
make up

# 3. Open Admin Console
open http://localhost:8081
```

---

## 🧪 Testing Guidelines

Before opening a pull request, ensure all test suites pass:

```bash
# Test Go Backend
cd backend && go test ./...

# Test Python Sandbox Agentd
cd ../sandbox/agentd && python -m unittest discover -p "test_*.py"

# Test Python SDK & CLI
cd ../../sdk/python && python -m unittest discover -s tests -p "test_*.py"

# Build React Admin Console
cd ../../admin && npm run build
```

---

## 📋 Code of Conduct & Contribution Flow

1. **Open an Issue**: For significant features or architectural changes, please open an issue to discuss design intent first.
2. **Branching**: Create a feature branch `git checkout -b feat/your-feature-name`.
3. **Commit Messages**: Follow standard conventional commit format (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`).
4. **Pull Requests**: Ensure all automated CI checks pass on your PR.
