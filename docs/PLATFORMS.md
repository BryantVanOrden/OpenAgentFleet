# Running on Linux, macOS and Windows

Every piece of OpenAgentFleet runs on all three, but not in the same way, and the
differences are worth knowing before you debug something that is working as
designed.

| Component | Linux | macOS | Windows | How |
| --- | :---: | :---: | :---: | --- |
| Orchestrator (Go) | ✅ | ✅ | ✅ | Runs in a Linux container on all three |
| Admin console | ✅ | ✅ | ✅ | A web app — any modern browser |
| Sandboxes | ✅ | ✅ | ✅ | Linux containers; on macOS/Windows they run inside the Docker Desktop VM |
| Companion app | ✅ | ✅ | ✅ | Flutter: Android, iOS, **and** Linux/macOS/Windows desktop |

The only hard requirement is **Docker with Compose v2**. The sandbox image is a
Linux desktop, so on macOS and Windows it runs inside the Docker Desktop VM —
which is fine, and is how you get a Linux agent desktop on a Mac.

---

## Linux

The native case; nothing special.

```bash
make doctor && make up
```

`make env` reads the group that owns `/var/run/docker.sock` and writes it to
`DOCKER_GID`, so the orchestrator can reach the socket without running as root.

## macOS

Docker Desktop or Colima. One difference that matters:

**`DOCKER_GID` should be `0`.** Inside the Docker Desktop VM the socket is owned
by root, and the group id you would read on the host is meaningless to it. If
you see the orchestrator fail to provision with a permission error on
`/var/run/docker.sock`, this is why:

```bash
# in .env
DOCKER_GID=0
```

Apple silicon builds the sandbox image for arm64 automatically. If you need the
x86 image (some tools are amd64-only), set `platform: linux/amd64` on the
sandbox build — expect it to be markedly slower under emulation.

## Windows

Docker Desktop with the **WSL 2** backend. Same `DOCKER_GID=0` note as macOS,
and for the same reason.

Two Windows-specific things worth stating plainly:

- **`DOCKER_HOST` must not be a named pipe.** The orchestrator talks to the
  engine over its REST API and does not implement `npipe://`. This is a
  non-issue in the normal setup, because the orchestrator runs *inside* Docker
  with `/var/run/docker.sock` bind-mounted and never sees a Windows path. It
  only bites if you try to run the Go binary directly on the host — in which
  case expose the engine on `tcp://` instead. The error message says so.
- **Run the repo from the Linux filesystem if you can.** Building from
  `/mnt/c/...` inside WSL is dramatically slower than from `~/` due to the
  9p filesystem bridge.

`make` targets assume a POSIX shell. Git Bash or WSL both work; the scripts are
plain `bash` and are tested on both.

---

## The companion app

```bash
cd mobile
flutter run -d linux      # or windows, macos
flutter run -d android    # or a connected iOS device
```

The app is triage-first — watch, chat, unblock, take over — so the desktop
builds are genuinely useful as a second-screen monitor beside the console
rather than a phone app forced into a window.

**Interactive takeover works on every platform, by two different engines.**
It embeds the noVNC client in a web view. `webview_flutter` covers Android, iOS
and macOS; Linux and Windows embed CEF instead through `webview_cef`, in the page
rather than in a window of their own, so the desktop tab behaves the same
everywhere. If neither engine is available the app falls back to opening the
console in the system browser.

That fallback is the fallback and not the primary path on purpose: depending on
the system browser proved fragile. On one machine the default handler pointed at
a Chromium sitting behind an unaccepted first-run dialog, so takeover opened
nothing and explained nothing.

| | Android | iOS | macOS | Linux | Windows |
| --- | :---: | :---: | :---: | :---: | :---: |
| Fleet, chat, alerts, run control | ✅ | ✅ | ✅ | ✅ | ✅ |
| Live single-frame desktop view | ✅ | ✅ | ✅ | ✅ | ✅ |
| Interactive takeover (embedded noVNC) | ✅ | ✅ | ✅ | ✅ (CEF) | ✅ (CEF) |

**Firebase is optional.** `Firebase.initializeApp()` failing is caught and the
app degrades to in-app alerts only, so it builds and runs with no
`google-services.json` present. Push notifications are the only thing you lose.

Building for a desktop target needs that platform's toolchain:

| Target | Needs |
| --- | --- |
| Linux | `clang`, `cmake`, `ninja-build`, `pkg-config`, `libgtk-3-dev` |
| macOS | Xcode command line tools |
| Windows | Visual Studio with the "Desktop development with C++" workload |

`flutter doctor` will tell you which of these you are missing.

---

## What is genuinely not portable

Stated plainly rather than discovered later:

- **The sandbox is Linux, always.** There is no Windows or macOS agent desktop.
  An agent driving Excel-on-Windows would need a different sandbox driver
  entirely; that is not built.
- **GPU passthrough is Linux-host only.** The `developer-heavy` tier's NVIDIA
  device request needs the container toolkit on a Linux host. On Docker Desktop
  the request is silently ignored and you get a CPU-only sandbox.
- **Disk quotas need overlay2 on XFS with pquota.** Everywhere else the limit is
  advisory. The orchestrator logs when it falls back rather than pretending.
- **`nftables` egress policy needs a Linux host kernel** that permits `NET_ADMIN`
  in the container. It works under Docker Desktop's VM, but if the policy cannot
  be applied the sandbox refuses to start rather than coming up unrestricted.
