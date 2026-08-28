#!/usr/bin/env bash
# Archetype & Workspace Initializer for AgentFleet Sandbox
# Runs during container startup to prepare the agent's work directory.
set -euo pipefail

WORK_DIR="/home/agent/work"
mkdir -p "${WORK_DIR}"
cd "${WORK_DIR}"

ARCHETYPE="${ARCHETYPE_ID:-general}"
TOOLS="${PREINSTALLED_TOOLS:-}"
REPOS="${PREINSTALLED_REPOS:-}"

echo "[init-archetype] Initializing workspace for archetype: ${ARCHETYPE}"

# 1. Generate Archetype README in the work directory
cat <<EOF > "${WORK_DIR}/ARCHETYPE_README.md"
# Agent Workspace (${ARCHETYPE})

This container sandbox has been provisioned and configured for archetype: **${ARCHETYPE}**.

## Pre-configured Tools
${TOOLS:-Standard desktop & CLI automation tools}

## Repositories
${REPOS:-None pre-cloned}

## Environment
- Home: /home/agent
- Work Directory: ${WORK_DIR}
- Python REPL: Persistent (accessible via 'repl' action or /repl endpoint)
- Permissions: sudo enabled (NOPASSWD) for on-demand package installs

EOF

chown agent:agent "${WORK_DIR}/ARCHETYPE_README.md" || true

# 2. Clone any specified pre-installed repositories
if [[ -n "${REPOS}" ]]; then
    IFS=',' read -ra REPO_LIST <<< "${REPOS}"
    for repo in "${REPO_LIST[@]}"; do
        repo_trimmed=$(echo "${repo}" | xargs)
        if [[ -n "${repo_trimmed}" ]]; then
            repo_name=$(basename "${repo_trimmed}" .git)
            if [[ ! -d "${WORK_DIR}/${repo_name}" ]]; then
                echo "[init-archetype] Cloning https://${repo_trimmed} into ${WORK_DIR}/${repo_name}..."
                # Clone as agent user, with fallback if offline
                su - agent -c "git clone --depth 1 'https://${repo_trimmed}' '${WORK_DIR}/${repo_name}'" || {
                    echo "[init-archetype] Warning: Could not clone https://${repo_trimmed} (network offline or repo private)"
                }
            fi
        fi
    done
fi

echo "[init-archetype] Workspace initialization complete."
