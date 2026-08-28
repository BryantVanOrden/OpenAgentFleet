#!/usr/bin/env bash
set -e

echo "================================================================================"
echo "                       ⚡ AGENTFLEET QUICKSTART BOOTSTRAP                       "
echo "================================================================================"

# 1. Check Docker
if command -v docker &> /dev/null; then
    echo "✅ Docker found."
else
    echo "⚠️  Docker not detected. Please install Docker to run sandboxed containers."
fi

# 2. Check Python & SDK
if command -v python3 &> /dev/null; then
    echo "✅ Python found. Installing SDK in editable mode..."
    pip install -e sdk/python --quiet
    echo "✅ fleetctl installed."
fi

# 3. Prompt for Docker Compose launch
read -p "Start local Docker Compose production stack now? (y/n): " choice
if [[ "$choice" == "y" || "$choice" == "Y" ]]; then
    echo "🐳 Launching AgentFleet production stack..."
    docker compose up -d
    echo "✅ AgentFleet is live at http://localhost:8080 (Admin Console: http://localhost:3000)"
else
    echo "Run 'docker compose up -d' whenever you are ready."
fi
