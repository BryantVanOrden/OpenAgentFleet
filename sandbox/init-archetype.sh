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

# 1. Install the archetype's requested tools.
#
# These used to be written into a README and nothing else, while the agent's
# system prompt told the model it HAD them. A bot provisioned as cyber_ops was
# told it had metasploit and ghidra and had neither, so it spent its steps
# invoking commands that did not exist. Anything installable is installed here;
# whatever is left is filtered out of the prompt by the orchestrator, so the
# model is only ever told about tools that are actually present.
# Recipes live in /etc/agentfleet/tools.conf so a tool can be added or its
# install method changed without touching this script or rebuilding anything
# that matters. Anything with no recipe is tried as an apt package of its own
# name; anything marked skip, or that simply fails, is left out and therefore
# never claimed to the agent.
RECIPES="/etc/agentfleet/tools.conf"

# Recipes the operator supplied with this bot, in the same format as the file.
# Written out so both sources are looked up the same way, and so an operator's
# addition behaves exactly like a built-in one.
OPERATOR_RECIPES="/run/agentfleet/custom-tools.conf"
if [[ -n "${CUSTOM_TOOL_RECIPES:-}" ]]; then
    mkdir -p "$(dirname "${OPERATOR_RECIPES}")"
    printf '%s\n' "${CUSTOM_TOOL_RECIPES}" > "${OPERATOR_RECIPES}"
fi

recipe_for() {
    local tool="$1" file
    # The operator's own recipes win, so a bot can override a built-in.
    for file in "${OPERATOR_RECIPES}" "${RECIPES}"; do
        [[ -r "${file}" ]] || continue
        # Fixed-string match on the key rather than a regex built from the
        # tool name: the name comes from a request, and building a pattern out
        # of it is how a sed expression ends up meaning something else.
        local line
        line="$(awk -F= -v want="${tool}" '
            { key=$1; gsub(/^[ \t]+|[ \t]+$/, "", key)
              if (key == want) { sub(/^[^=]*=[ \t]*/, ""); print; exit } }
        ' "${file}")"
        if [[ -n "${line}" ]]; then
            printf '%s' "${line}"
            return 0
        fi
    done
    return 1
}

install_one() {
    local tool="$1" spec="$2" method arg
    method="${spec%%:*}"
    arg="${spec#*:}"
    [[ "${method}" == "${spec}" ]] && arg=""

    case "${method}" in
        skip)  return 1 ;;
        apt)   timeout 180 apt-get install -y --no-install-recommends "${arg:-$tool}" >/dev/null 2>&1 ;;
        pip)   timeout 300 pip3 install --no-cache-dir --break-system-packages "${arg}" >/dev/null 2>&1 ;;
        npm)   command -v npm >/dev/null 2>&1 || timeout 180 apt-get install -y --no-install-recommends npm >/dev/null 2>&1
               timeout 300 npm install -g "${arg}" >/dev/null 2>&1 ;;
        go)    command -v go >/dev/null 2>&1 || timeout 180 apt-get install -y --no-install-recommends golang-go >/dev/null 2>&1
               GOBIN=/usr/local/bin timeout 420 go install "${arg}" >/dev/null 2>&1 ;;
        url)   timeout 180 curl -fsSL "${arg}" -o "/usr/local/bin/${tool}" >/dev/null 2>&1 \
                   && chmod +x "/usr/local/bin/${tool}" ;;
        tgz)   local url="${arg%%#*}" path="${arg##*#}" tmp
               tmp="$(mktemp -d)"
               timeout 240 curl -fsSL "${url}" -o "${tmp}/a.tgz" >/dev/null 2>&1 \
                   && tar -xzf "${tmp}/a.tgz" -C "${tmp}" >/dev/null 2>&1 \
                   && install -m755 "${tmp}/${path}" "/usr/local/bin/${tool}" >/dev/null 2>&1
               local rc=$?; rm -rf "${tmp}"; return $rc ;;
        github)
               # Clone into the workspace. Named tools that are really source
               # trees — wordlists, templates — are useful to have on disk even
               # though nothing lands on PATH.
               local dest="/home/agent/work/${tool}"
               [[ -d "${dest}" ]] && return 0
               su - agent -c "git clone --depth 1 'https://github.com/${arg}' '${dest}'" >/dev/null 2>&1 ;;
        sh)    # Only ever from the image's own recipe file, never from a
               # request: the operator-supplied methods are the fixed set
               # validated server-side, and sh is not among them.
               timeout 420 bash -c "${arg}" >/dev/null 2>&1 ;;
        *)     timeout 180 apt-get install -y --no-install-recommends "${tool}" >/dev/null 2>&1 ;;
    esac
}

install_tools() {
    local tools="$1"
    [[ -z "${tools}" ]] && return 0

    apt-get update -qq >/dev/null 2>&1 || true

    IFS=',' read -ra WANTED <<< "${tools}"
    for raw in "${WANTED[@]}"; do
        local t; t="$(echo "${raw}" | xargs)"
        [[ -z "${t}" ]] && continue
        command -v "${t}" >/dev/null 2>&1 && continue

        # recipe_for reports "no recipe" by exit status, which under `set -e`
        # took the whole initializer with it -- so the apt fallback below was
        # unreachable, and so were the README, the repo clones and the
        # .tools-ready marker the orchestrator waits on.
        local spec; spec="$(recipe_for "${t}")" || spec=""
        [[ -z "${spec}" ]] && spec="apt:${t}"

        if [[ "${spec}" == "skip" ]]; then
            echo "[init-archetype]   ${t}: no unattended install"
            continue
        fi
        if install_one "${t}" "${spec}"; then
            echo "[init-archetype]   ${t}: installed (${spec%%:*})"
        else
            echo "[init-archetype]   ${t}: unavailable (${spec%%:*})"
        fi
    done

    rm -rf /var/lib/apt/lists/* 2>/dev/null || true
}

install_tools "${TOOLS}"
install_tools "${CUSTOM_TOOLS:-}"
# The orchestrator waits for this before deciding which tools the agent has,
# so it must be written once installation is finished either way.
date -u +%FT%TZ > "${WORK_DIR}/.tools-ready" 2>/dev/null || true

# 1b. Generate Archetype README in the work directory
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
- Permissions: sudo is available only if the operator granted it to this bot.
  Check with \`sudo -n true\` before relying on it.

EOF

chown agent:agent "${WORK_DIR}/ARCHETYPE_README.md" || true

# 2. Clone any specified pre-installed repositories, in the background.
#
# Deliberately not blocking: this script runs before supervisord starts, so
# anything slow here delays agentd and the orchestrator gives up waiting for
# the sandbox to answer. Some archetypes list repositories measured in
# gigabytes — cyber_ops pulls SecLists — and a machine that never finishes
# provisioning is worse than one whose wordlists arrive a minute late.
clone_repos() {
    local repos="$1"
    [[ -z "${repos}" ]] && return 0

    IFS=',' read -ra REPO_LIST <<< "${repos}"
    for repo in "${REPO_LIST[@]}"; do
        local repo_trimmed; repo_trimmed=$(echo "${repo}" | xargs)
        [[ -z "${repo_trimmed}" ]] && continue
        local repo_name; repo_name=$(basename "${repo_trimmed}" .git)
        [[ -d "${WORK_DIR}/${repo_name}" ]] && continue

        echo "[init-archetype] cloning ${repo_trimmed}"
        if su - agent -c "git clone --depth 1 'https://${repo_trimmed}' '${WORK_DIR}/${repo_name}'" >/dev/null 2>&1; then
            echo "[init-archetype]   cloned ${repo_name}"
        else
            echo "[init-archetype]   could not clone ${repo_trimmed} (offline or private)"
        fi
    done
    # A marker so anyone wondering whether the workspace is still filling in
    # can tell, rather than guessing from directory sizes.
    date -u +%FT%TZ > "${WORK_DIR}/.repos-ready" 2>/dev/null || true
}

if [[ -n "${REPOS}" ]]; then
    clone_repos "${REPOS}" &
    echo "[init-archetype] repository clones running in the background"
fi

echo "[init-archetype] Workspace initialization complete."
