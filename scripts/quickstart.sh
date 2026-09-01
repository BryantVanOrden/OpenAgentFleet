#!/usr/bin/env bash
# ==============================================================================
# OpenAgentFleet 1-Click Interactive Quickstart & Bootstrap Script (Linux / macOS)
# ==============================================================================
set -euo pipefail

CYAN='\033[0;36m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${CYAN}"
cat << "EOF"
    ___                    __   ______ __            __ 
   /   | ____ _ ___  ____  / /_ / ____// /___   ___  / /_
  / /| |/ __ `// _ \/ __ \/ __// /_   / // _ \ / _ \/ __/
 / ___ / /_/ //  __/ / / / /_ / __/  / //  __//  __/ /_  
/_/  |_\__, / \___/_/ /_/\__//_/    /_/ \___/ \___/\__/  
      /____/                                             
EOF
echo -e "🚀 Autonomous Multi-Agent Desktop Fleet Orchestration Platform"
echo -e "==============================================================${NC}\n"

# 1. Check Docker Daemon
echo -n "🔍 Checking Docker installation and daemon... "
if ! command -v docker &> /dev/null; then
    echo -e "${RED}FAILED${NC}"
    echo -e "${RED}Error: Docker is not installed. Please install Docker and Docker Compose before running OpenAgentFleet.${NC}"
    exit 1
fi

if ! docker info &> /dev/null; then
    echo -e "${RED}FAILED${NC}"
    echo -e "${RED}Error: Docker daemon is not running. Please start Docker.${NC}"
    exit 1
fi
echo -e "${GREEN}OK${NC}"

# 2. Check or Generate .env Configuration
if [ ! -f .env ]; then
    echo -e "⚙️  No .env file found. Generating production-grade cryptographic secrets..."
    
    gen_secret() {
        if command -v openssl &> /dev/null; then
            openssl rand -hex 32
        else
            python3 -c "import secrets; print(secrets.token_hex(32))" 2>/dev/null || cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 64 | head -n 1
        fi
    }

    MASTER_KEY=$(gen_secret)
    JWT_SECRET=$(gen_secret)
    PG_PASS=$(gen_secret | cut -c1-24)

    cat > .env << EOF
# OpenAgentFleet Production Environment Configuration
PORT=8080
BASE_URL=http://localhost:8080
MASTER_KEY=${MASTER_KEY}
JWT_SECRET=${JWT_SECRET}
DATABASE_URL=postgres://agentfleet:${PG_PASS}@postgres:5432/agentfleet?sslmode=disable
POSTGRES_USER=agentfleet
POSTGRES_PASSWORD=${PG_PASS}
POSTGRES_DB=agentfleet
HOST_GATEWAY=host.docker.internal
EOF
    echo -e "${GREEN}✔ Created .env with sealed AES-256 MASTER_KEY and JWT_SECRET.${NC}"
else
    echo -e "${GREEN}✔ Existing .env file found.${NC}"
fi

# 3. Boot Full Fleet Stack
echo -e "\n🐳 Starting OpenAgentFleet stack via Docker Compose..."
docker compose up -d

echo -e "\n${GREEN}==============================================================${NC}"
echo -e "${GREEN}🎉 OpenAgentFleet is up and running!${NC}"
echo -e "${GREEN}==============================================================${NC}"
echo -e "🖥️  Web Admin Console:   ${CYAN}http://localhost:5173${NC}"
echo -e "⚡ Orchestrator API:     ${CYAN}http://localhost:8080${NC}"
echo -e "📱 Health Endpoint:      ${CYAN}http://localhost:8080/healthz${NC}"
echo -e "\n💡 Tip: Install the Python SDK and CLI tool:"
echo -e "   ${YELLOW}pip install -e ./sdk/python${NC}"
echo -e "   ${YELLOW}fleetctl doctor${NC}\n"
