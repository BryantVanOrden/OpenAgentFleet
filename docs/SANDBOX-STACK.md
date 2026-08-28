# Sandbox stack

What belongs in the desktop image, what does not, and the exact configuration to
make it usable by an agent rather than by a person. Claims are cited; anything I
could not verify is tagged **unverified**.

This describes a target. `sandbox/Dockerfile` today ships Ubuntu 24.04 + XFCE +
Xvfb + x11vnc + noVNC + Firefox + xdotool/wmctrl/scrot/imagemagick + AT-SPI +
a venv for agentd, and nothing else — no file manager, no editor, no viewer, no
office suite, no fonts beyond DejaVu and Liberation.

## 1. What agents are actually asked to do

OSWorld's 370 published task files, counted directly from the repository
([xlang-ai/OSWorld `evaluation_examples/examples`](https://github.com/xlang-ai/OSWorld/tree/main/evaluation_examples/examples)):

| Domain | Tasks | Share |
|---|---|---|
| multi_apps | 102 | 27.6% |
| libreoffice_calc | 47 | 12.7% |
| libreoffice_impress | 47 | 12.7% |
| chrome | 46 | 12.4% |
| gimp | 26 | 7.0% |
| os (file management, settings, terminal) | 24 | 6.5% |
| vs_code | 23 | 6.2% |
| libreoffice_writer | 23 | 6.2% |
| vlc | 17 | 4.6% |
| thunderbird | 15 | 4.1% |

**LibreOffice is 31.6% of the benchmark — two and a half times the browser.**
That is the single most important number in this document, and our image has no
office suite at all. The browser-only mental model of a computer-use agent is
wrong for a desktop product.

Two reference images for comparison:

- **Anthropic's computer-use demo** installs `xvfb xterm xdotool scrot
  imagemagick sudo mutter x11vnc build-essential curl git net-tools netcat
  software-properties-common libreoffice firefox-esr x11-apps xpdf gedit xpaint
  tint2 galculator pcmanfm unzip`, plus noVNC 1.5.0 and websockify 0.12.0
  ([Dockerfile](https://github.com/anthropics/claude-quickstarts/blob/main/computer-use-demo/Dockerfile)).
  Mutter + tint2, not a full desktop environment. **No AT-SPI at all** — it is a
  pure-vision agent. Our accessibility layer is a genuine differentiator, and one
  we should not give up to save disk.
- **E2B desktop** is Ubuntu 22.04 + `xfce4 xfce4-goodies libreoffice xpdf gedit
  xpaint tint2 galculator pcmanfm ffmpeg xdotool scrot x11vnc` plus Firefox ESR,
  Google Chrome and VS Code
  ([template.py](https://github.com/e2b-dev/desktop/blob/main/template/template.py)).
  Its Firefox preseeding is reproduced and improved on in §6.

---

## 2. Base image package list

```dockerfile
# --- X, session, remote view -------------------------------------------------
RUN apt-get update && apt-get install -y --no-install-recommends \
        xvfb xauth x11-xserver-utils x11-utils \
        xfwm4 xfce4-session xfce4-panel xfce4-settings xfdesktop4 \
        xfce4-terminal thunar thunar-archive-plugin \
        dbus-x11 at-spi2-core \
        x11vnc novnc websockify supervisor \
# --- automation + accessibility ----------------------------------------------
        xdotool wmctrl xclip xsel scrot imagemagick \
        python3 python3-venv python3-gi python3-pyatspi gir1.2-atspi-2.0 \
        tesseract-ocr tesseract-ocr-eng \
# --- fonts (see §7) ----------------------------------------------------------
        fonts-dejavu-core fonts-liberation2 \
        fonts-noto-core fonts-noto-cjk fonts-noto-color-emoji \
        fontconfig \
# --- applications the tasks actually need ------------------------------------
        mousepad \
        atril ristretto \
        xarchiver p7zip-full unzip zip \
        libreoffice-writer libreoffice-calc libreoffice-impress \
        libreoffice-gtk3 \
# --- plumbing ----------------------------------------------------------------
        ca-certificates curl wget gnupg nftables iproute2 less nano \
    && apt-get purge -y --auto-remove \
        xfce4-power-manager xfce4-screensaver light-locker xscreensaver \
        xfce4-notifyd gnome-keyring xdg-desktop-portal xdg-desktop-portal-gtk \
        2>/dev/null || true \
    && rm -rf /var/lib/apt/lists/*
```

Note the switch from the `xfce4` metapackage to its four real components.
`xfce4` pulls in the power manager, the screensaver, `xfce4-appfinder`, PulseAudio
plugins and a notification daemon — every one of which is either a modal an agent
must dismiss or dead weight in a headless container.

## 3. `-dev` image (`developer-heavy` tier)

```dockerfile
FROM agentfleet/sandbox:base
RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential cmake ninja-build pkg-config ccache \
        git git-lfs \
        python3-dev python3-pip \
        nodejs npm \
        default-jdk libatk-wrapper-java libatk-wrapper-java-jni \
        ffmpeg \
        libgl1 libglu1-mesa libx11-dev libxi-dev libxrandr-dev \
        libxcursor-dev libxinerama-dev libasound2-dev libudev-dev \
        gdb strace \
    && rm -rf /var/lib/apt/lists/*
# Rust and Go via rustup/tarball rather than apt: apt versions rot, and the
# toolchain is what a build actually pins.
```

The X11/GL/ALSA `-dev` packages are Godot's documented Linux build dependencies
and are also what most SDL-based engines need; they are cheap (~120 MB) and
useless to install on demand because they are what an on-demand install would
need first.

**Game engines: do not bake any of them in.**

- **Godot** is a single self-contained binary. Published headless CI images run
  **956 MB – 1.39 GB** depending on version and which export templates are
  included ([robpc/godot-headless](https://hub.docker.com/r/robpc/godot-headless),
  [barichello/godot-ci](https://hub.docker.com/r/barichello/godot-ci)). Fetch the
  binary and the matching export templates on demand into `/home/agent/work`.
  That is one `curl` and one `unzip` the agent can do itself.
- **Unity cannot be licensed inside a container without work we cannot do
  generically.** Unity's licence file is bound to the machine ID and hostname,
  and Docker masks `/etc/machine-id`
  ([Unity issue tracker](https://issuetracker.unity3d.com/issues/cannot-activate-license-within-a-docker-container),
  [GameCI activation](https://game.ci/docs/gitlab/activation/)). If Unity is
  genuinely required, use a GameCI `unityci/editor` image as a separate tier
  image with an operator-supplied licence — not the general sandbox.
- **Unreal** requires Epic-account-gated container images and a hundred-plus GB
  of disk; Epic documents Linux container images as buildable only under Linux
  ([Epic docs](https://dev.epicgames.com/documentation/unreal-engine/building-the-linux-container-images-from-source)).
  Out of scope for a disposable sandbox. Point it at a dedicated build host.

The general rule: **a toolchain that every dev task needs goes in the image; a
toolchain that one task needs is fetched by the agent.** With `ALLOW_SHELL=true`
the agent can install anything; without it, the agent could not use a baked
toolchain either.

---

## 4. Browser: Firefox, and why not Chromium

**Recommendation: keep Firefox as the default; add Chromium only to `-dev`.**

**Accessibility is the deciding factor.** Firefox's accessibility engine is
controlled by `accessibility.force_disabled`, documented in the source as:

> `-1` force enabled (accessibility should always be enabled), `0` enabled (will
> be started upon a request, **default value**), `1` force disabled
> ([`accessible/base/nsAccessibilityService.cpp`](https://github.com/mozilla-firefox/firefox/blob/main/accessible/base/nsAccessibilityService.cpp))

The default is **on demand**. Our `agentd` is an AT client via `pyatspi`, so in
practice the engine should activate — but "should" is not a property to build a
product on, and activation ordering with a container's D-Bus startup is exactly
the kind of race that produces an empty tree once a week. **Set
`accessibility.force_disabled` to `-1`.** Our current Dockerfile sets
`GNOME_ACCESSIBILITY=1` and `NO_AT_BRIDGE=0` but never forces Firefox's engine
on; that is a real gap.

Chromium exposes AT-SPI only when renderer accessibility is enabled, normally via
`--force-renderer-accessibility` (**unverified** whether Ubuntu's Chromium snap
transition package can be avoided the way Firefox's can — Ubuntu 24.04 ships
`chromium-browser` as a snap stub, and there is no vendor apt repository
equivalent to Mozilla's, so a real Chromium `.deb` means a third-party
repository).

Other reasons to stay on Firefox: we already solved the snap problem with
Mozilla's apt repository and the apt pin, its enterprise policy surface is a
single documented JSON file, and its policy schema covers every first-run nag in
one place. Chromium earns its space only where a task specifically needs
Blink — hence `-dev` only.

### 4.1 `/etc/firefox/policies/policies.json`

The Mozilla policy templates state the file goes in `firefox/distribution` under
the install directory, **or system-wide in `/etc/firefox/policies`**
([policy-templates](https://mozilla.github.io/policy-templates/)). Use the
system-wide path — it survives a Firefox package upgrade, which the
`distribution/` path does not.

```json
{
  "policies": {
    "OverrideFirstRunPage": "",
    "OverridePostUpdatePage": "",
    "DisableProfileImport": true,
    "DisableProfileRefresh": true,
    "DontCheckDefaultBrowser": true,
    "DisableSetDesktopBackground": true,
    "NoDefaultBookmarks": true,
    "DisableAppUpdate": true,
    "ExtensionUpdate": false,
    "DisableTelemetry": true,
    "DisableFirefoxStudies": true,
    "DisableFeedbackCommands": true,
    "DisableFirefoxAccounts": true,
    "DisablePocket": true,
    "DisableFormHistory": true,
    "OfferToSaveLogins": false,
    "PasswordManagerEnabled": false,
    "PromptForDownloadLocation": false,
    "CaptivePortal": false,
    "NetworkPrediction": false,
    "DNSOverHTTPS": { "Enabled": false, "Locked": true },
    "PDFjs": { "Enabled": true, "EnablePermissions": false },
    "UserMessaging": {
      "WhatsNew": false,
      "ExtensionRecommendations": false,
      "FeatureRecommendations": false,
      "UrlbarInterventions": false,
      "SkipOnboarding": true,
      "MoreFromMozilla": false,
      "Locked": true
    },
    "Permissions": {
      "Notifications": { "BlockNewRequests": true, "Locked": true },
      "Location":      { "BlockNewRequests": true, "Locked": true },
      "Camera":        { "BlockNewRequests": true, "Locked": true },
      "Microphone":    { "BlockNewRequests": true, "Locked": true }
    },
    "Homepage": { "URL": "about:blank", "StartPage": "homepage", "Locked": false }
  }
}
```

Every key above is verified against the policy templates index. `Permissions.*.
BlockNewRequests` is the one that matters most in practice: an "Allow
notifications?" doorhanger is a modal an agent will click at random.

### 4.2 Autoconfig for what policy cannot reach

Policy's `Preferences` key accepts only an allowlisted set of preference
prefixes (**unverified**: I could not retrieve the current allowlist from the
policy templates page). Autoconfig has no such restriction, so put the
pref-level suppressions there and treat policy as the belt.

`/usr/lib/firefox/defaults/pref/autoconfig.js`:

```js
pref("general.config.filename", "agentfleet.cfg");
pref("general.config.obscure_value", 0);
```

`/usr/lib/firefox/agentfleet.cfg` — **the first line must be a comment, it is
skipped by the parser**:

```js
// AgentFleet sandbox preferences. First line is intentionally ignored.

// Accessibility: force the engine on rather than waiting for an AT client.
lockPref("accessibility.force_disabled", -1);

// First run, onboarding, what's-new
lockPref("browser.startup.homepage_override.mstone", "ignore");
pref("browser.startup.homepage_override.buildID", "");
lockPref("browser.aboutwelcome.enabled", false);
lockPref("browser.messaging-system.whatsNewPanel.enabled", false);
lockPref("browser.shell.checkDefaultBrowser", false);
lockPref("browser.startup.page", 0);
lockPref("browser.startup.homepage", "about:blank");
lockPref("browser.newtabpage.enabled", false);

// Crash / session restore prompt: never offer to restore, never ask on quit
lockPref("browser.sessionstore.resume_from_crash", false);
lockPref("browser.sessionstore.max_resumed_crashes", 0);
lockPref("browser.tabs.warnOnClose", false);
lockPref("browser.tabs.warnOnCloseOtherTabs", false);
lockPref("browser.warnOnQuit", false);
lockPref("browser.warnOnQuitShortcut", false);

// Downloads: never open the save dialog, never ask what to do with a file type
lockPref("browser.download.useDownloadDir", true);
lockPref("browser.download.folderList", 2);
lockPref("browser.download.dir", "/home/agent/work");
lockPref("browser.download.always_ask_before_handling_new_types", false);
lockPref("browser.download.alwaysOpenPanel", false);
lockPref("browser.download.panel.shown", true);

// Telemetry / studies / suggest — belt and braces with the policy file
lockPref("toolkit.telemetry.enabled", false);
lockPref("toolkit.telemetry.unified", false);
lockPref("toolkit.telemetry.archive.enabled", false);
lockPref("datareporting.healthreport.uploadEnabled", false);
lockPref("datareporting.policy.dataSubmissionEnabled", false);
lockPref("app.shield.optoutstudies.enabled", false);
lockPref("app.normandy.enabled", false);
lockPref("app.update.auto", false);
lockPref("extensions.pocket.enabled", false);
lockPref("browser.urlbar.suggest.quicksuggest.sponsored", false);
lockPref("browser.urlbar.suggest.quicksuggest.nonsponsored", false);
lockPref("browser.newtabpage.activity-stream.showSponsored", false);
lockPref("browser.newtabpage.activity-stream.showSponsoredTopSites", false);
lockPref("browser.newtabpage.activity-stream.feeds.section.topstories", false);
lockPref("extensions.htmlaboutaddons.recommendations.enabled", false);
lockPref("browser.discovery.enabled", false);

// Modals an agent cannot usefully answer
lockPref("full-screen-api.warning.timeout", 0);
lockPref("dom.push.enabled", false);
lockPref("permissions.default.desktop-notification", 2);
lockPref("geo.enabled", false);
lockPref("signon.rememberSignons", false);
lockPref("signon.autofillForms", false);
lockPref("browser.formfill.enable", false);
lockPref("browser.contentblocking.report.hide_vpn_banner", true);
```

The prefs list is an extension of E2B's working `firefox.cfg`
([e2b-dev/desktop](https://github.com/e2b-dev/desktop/blob/main/template/files/firefox.cfg));
the download, session-restore, accessibility and permission blocks are additions.

### 4.3 Chromium, if it is added to `-dev`

`/etc/chromium/policies/managed/agentfleet.json`
(**unverified** against the current Chrome Enterprise policy list — key names are
from memory and must be checked before shipping):

```json
{
  "PasswordManagerEnabled": false,
  "AutofillAddressEnabled": false,
  "AutofillCreditCardEnabled": false,
  "DefaultNotificationsSetting": 2,
  "DefaultGeolocationSetting": 2,
  "MetricsReportingEnabled": false,
  "PromptForDownloadLocation": false,
  "DownloadDirectory": "/home/agent/work",
  "BrowserSignin": 0,
  "SyncDisabled": true,
  "TranslateEnabled": false,
  "DefaultBrowserSettingEnabled": false,
  "ComponentUpdatesEnabled": false
}
```

Flags that matter in a container:
`--no-first-run --no-default-browser-check --disable-features=Translate,MediaRouter
--password-store=basic --disable-dev-shm-usage --force-renderer-accessibility
--test-type --start-maximized`. `--disable-dev-shm-usage` is only needed if the
tier's `ShmMB` is small; ours are 256 MB–4 GB, so the `power-user` and
`developer-heavy` tiers do not need it.

---

## 5. Applications: what earns its disk

| Ship | Why |
|---|---|
| **LibreOffice Writer/Calc/Impress + `libreoffice-gtk3`** | 31.6% of OSWorld. The GTK3 VCL plugin is what gives LibreOffice an AT-SPI tree at all; without it VCL draws its own widgets and the accessibility tree is close to empty (**unverified** for LibreOffice 24.2 specifically). Set `SAL_USE_VCLPLUGIN=gtk3`. |
| **Thunar** + `thunar-archive-plugin` | File management is 6.5% of OSWorld directly and threaded through most of `multi_apps`. Thunar is the XFCE-native manager, so it shares the GTK a11y stack we already have. |
| **xfce4-terminal** | Already present. The single highest-leverage app in the image when shell is enabled. |
| **Mousepad** | ~2 MB GTK text editor with a real AT-SPI tree. `gedit` pulls a much larger GNOME dependency set for the same job. |
| **Atril** | PDF viewer, GTK, MATE's fork of Evince. `xpdf` (which both reference images ship) is Motif — **no AT-SPI whatsoever**, so an agent is reduced to pixels inside it. This is a case where the reference images made the wrong call for our architecture. |
| **Ristretto** | XFCE image viewer, ~1 MB, GTK. |
| **xarchiver + p7zip-full + unzip + zip** | Archives arrive constantly (downloads, exports, build artefacts). `unzip`/`zip`/`p7zip` are the ones that matter; `xarchiver` is the GUI for when shell is off. |
| **tesseract-ocr + tesseract-ocr-eng** | ~15 MB (**approximate**). Phase 8 of `ROADMAP.md` wants an OCR fallback for canvas/video/game windows where AT-SPI is blind, and that fallback is what makes `wait_for` and `assert` work outside a11y-exposing apps. Install it now so the feature is a code change, not an image change. |
| **xclip + xsel** | `xclip` is already installed and completely unused. The clipboard is how an agent reads a long value off the screen without OCR. |

### Deliberately excluded

| Excluded | Reason |
|---|---|
| `xfce4` metapackage / `xfce4-goodies` | Pulls the power manager, screensaver, notification daemon, appfinder, mixer and a dozen panel plugins. Every one is either an agent-visible modal or dead weight. Install the four real components. |
| GIMP | 7% of OSWorld, ~350 MB (**approximate**), and its canvas is the least accessible surface in the image. A poor trade until OCR-based grounding lands. Put it in `-dev` if an image-editing workload appears. |
| VLC | 4.6% of OSWorld, and its output is a video surface an agent cannot read. `ffmpeg` in `-dev` does anything a task actually needs to a media file. |
| Thunderbird | 4.1% of OSWorld, but a mail client is a credential-handling app, and the security model says credentials go through the keyring, not through a GUI login the agent drives. Excluded on the threat model, not on size. |
| VS Code | Electron; ~350 MB; its AT-SPI exposure is partial at best. The `-dev` tier has `git`, a compiler and a terminal, which is what a build task needs. |
| Full `libreoffice` metapackage | Adds Base, Draw, Math, the Java stack and every language pack for use cases OSWorld does not contain. |
| `gnome-keyring` | Produces a "unlock your keyring" modal on first credential use and has no keyring worth unlocking in a disposable container. Our credentials come through `agentd`'s tmpfs keyring. |
| `xdg-desktop-portal*` | Portal-mediated file choosers add a D-Bus round trip and a second dialog implementation for no benefit here. |
| `ttf-mscorefonts-installer` | EULA prompt at install time and a network fetch from SourceForge in every build. Liberation covers the same metrics. |
| `fonts-noto-cjk-extra` | The base `fonts-noto-cjk` carries regular and bold; `-extra` adds the remaining weights ([packages.ubuntu.com](https://packages.ubuntu.com/noble/fonts-noto-cjk)). Weights are not what stops tofu. |
| Snap, systemd, PulseAudio | Nothing in a headless single-purpose container needs them. |

---

## 6. Fonts

Missing glyphs render as tofu boxes, which break OCR and mislead vision
grounding — a `□□□` button label is worse than no label, because the model will
confidently invent one.

| Package | Covers | Installed size |
|---|---|---|
| `fonts-dejavu-core` | Latin/Greek/Cyrillic UI default | ~3 MB (**approximate**) |
| `fonts-liberation2` | Metric-compatible Arial/Times/Courier — required for Office documents to lay out correctly | ~4 MB (**approximate**) |
| `fonts-noto-core` | Broad Unicode: Arabic, Hebrew, Devanagari, Thai, symbols | ~40 MB (**approximate**) |
| `fonts-noto-cjk` | Chinese, Japanese, Korean, regular + bold | **91 MB** ([verified](https://packages.ubuntu.com/noble/fonts-noto-cjk)) |
| `fonts-noto-color-emoji` | Emoji | ~10 MB (**approximate**) |

`fonts-noto-cjk` is the largest single font cost and the one most likely to be
cut. Do not cut it: CJK tofu in a spreadsheet is silent data corruption from the
model's point of view. If the fleet is provably Latin-only, drop it and save
91 MB — but make that an explicit build argument, not a default.

Emoji need a fontconfig rule to be selected at all, since Noto Color Emoji has no
Latin coverage and so never wins a normal match. `/etc/fonts/conf.d/99-agentfleet.conf`:

```xml
<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "fonts.dtd">
<fontconfig>
  <alias><family>sans-serif</family><prefer>
    <family>DejaVu Sans</family>
    <family>Noto Sans</family>
    <family>Noto Sans CJK SC</family>
    <family>Noto Color Emoji</family>
  </prefer></alias>
  <alias><family>serif</family><prefer>
    <family>DejaVu Serif</family>
    <family>Noto Serif</family>
    <family>Noto Serif CJK SC</family>
    <family>Noto Color Emoji</family>
  </prefer></alias>
  <alias><family>monospace</family><prefer>
    <family>DejaVu Sans Mono</family>
    <family>Noto Sans Mono</family>
    <family>Noto Color Emoji</family>
  </prefer></alias>
  <!-- Sub-pixel rendering is meaningless on a virtual framebuffer and makes
       screenshot text harder for a vision model to read. Grey antialiasing. -->
  <match target="font">
    <edit name="rgba" mode="assign"><const>none</const></edit>
    <edit name="antialias" mode="assign"><bool>true</bool></edit>
    <edit name="hinting" mode="assign"><bool>true</bool></edit>
    <edit name="hintstyle" mode="assign"><const>hintslight</const></edit>
  </match>
</fontconfig>
```

Run `fc-cache -f` in the same layer.

---

## 7. Accessibility completeness

This determines how much of the recorder works and how much of the a11y tree the
agent sees. Current Dockerfile env is close to right; the gaps are per-toolkit.

| Toolkit | Exposes AT-SPI | What is needed |
|---|---|---|
| GTK 3 | Yes | `at-spi2-core` (which absorbed the `at-spi2-atk` bridge in 2.46+; Ubuntu 24.04 ships 2.52 — **unverified** that no separate `libatk-adaptor` is still required). `GTK_MODULES=gail:atk-bridge` is harmless legacy and can stay. |
| GTK 4 | Yes, natively | No bridge package; GTK 4 implements AT-SPI directly (**unverified** for the exact GTK version in 24.04). |
| Qt 5 / Qt 6 | Yes, with a flag | `QT_ACCESSIBILITY=1` and `QT_LINUX_ACCESSIBILITY_ALWAYS_ON=1` — both already set in our Dockerfile. |
| Electron | Partial | Renderer accessibility is off until requested; `--force-renderer-accessibility` turns it on. Chromium's own tree is exposed the same way. Coverage of custom-drawn UI is poor regardless. |
| Java / Swing | Yes, with a wrapper | `libatk-wrapper-java` + `libatk-wrapper-java-jni`, and `assistive_technologies=org.GNOME.Accessibility.AtkWrapper` in `$JAVA_HOME/conf/accessibility.properties`. Included in `-dev` above. **Unverified** that this still works on OpenJDK 21. |
| LibreOffice | Yes, through GTK3 VCL | `libreoffice-gtk3` + `SAL_USE_VCLPLUGIN=gtk3`. |
| Firefox | On demand by default | `accessibility.force_disabled = -1` (verified from source, §4.2). |
| SDL / OpenGL / game engines / video | No | Nothing exposes a tree. This is what OCR is for. |

Additional environment for the session, all of which belong on the `xfce`,
`agentd` and any app-launching process in `supervisord.conf`:

```
GTK_MODULES=gail:atk-bridge
QT_ACCESSIBILITY=1
QT_LINUX_ACCESSIBILITY_ALWAYS_ON=1
GNOME_ACCESSIBILITY=1
NO_AT_BRIDGE=0
SAL_USE_VCLPLUGIN=gtk3
JAVA_TOOL_OPTIONS=-Djavax.accessibility.assistive_technologies=org.GNOME.Accessibility.AtkWrapper
```

The single-session-D-Bus discipline already in `supervisord.conf` is the
prerequisite for all of it and is correct as written.

---

## 8. Anti-annoyance hardening

Preseed **system-wide** defaults in `/etc/xdg/xfce4/xfconf/xfce-perchannel-xml/`
rather than in `/home/agent/.config`, so a reset home directory does not
resurrect the dialogs.

**`xfce4-session.xml`** — no session save, no logout confirmation, no restore:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<channel name="xfce4-session" version="1.0">
  <property name="general" type="empty">
    <property name="SaveOnExit"   type="bool" value="false"/>
    <property name="AutoSave"     type="bool" value="false"/>
    <property name="PromptOnLogout" type="bool" value="false"/>
    <property name="LockCommand"  type="empty"/>
  </property>
  <property name="shutdown" type="empty">
    <property name="LockScreen" type="bool" value="false"/>
  </property>
  <property name="startup" type="empty">
    <property name="ssh-agent" type="empty">
      <property name="enabled" type="bool" value="false"/>
    </property>
    <property name="gpg-agent" type="empty">
      <property name="enabled" type="bool" value="false"/>
    </property>
  </property>
  <property name="security" type="empty">
    <property name="EnableTcp" type="bool" value="false"/>
  </property>
</channel>
```

**`xfwm4.xml`** — no compositing (meaningless and slow on Xvfb, and it makes
screenshots capture stale contents in some drivers):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<channel name="xfwm4" version="1.0">
  <property name="general" type="empty">
    <property name="use_compositing"  type="bool" value="false"/>
    <property name="click_to_focus"   type="bool" value="true"/>
    <property name="focus_new"        type="bool" value="true"/>
    <property name="snap_to_windows"  type="bool" value="false"/>
    <property name="snap_to_border"   type="bool" value="false"/>
    <property name="workspace_count"  type="int"  value="1"/>
  </property>
</channel>
```

One workspace matters: an agent that accidentally switches workspace sees an
empty desktop and has no idea why.

**`xfce4-desktop.xml`** — plain background, no desktop icons, no right-click menu
surprises:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<channel name="xfce4-desktop" version="1.0">
  <property name="desktop-icons" type="empty">
    <property name="style" type="int" value="0"/>
  </property>
  <property name="desktop-menu" type="empty">
    <property name="show" type="bool" value="false"/>
  </property>
  <property name="backdrop" type="empty">
    <property name="screen0" type="empty">
      <property name="monitor0" type="empty">
        <property name="workspace0" type="empty">
          <property name="color-style" type="int" value="0"/>
          <property name="image-style" type="int" value="0"/>
          <property name="rgba1" type="array">
            <value type="double" value="0.16"/><value type="double" value="0.18"/>
            <value type="double" value="0.20"/><value type="double" value="1.0"/>
          </property>
        </property>
      </property>
    </property>
  </property>
</channel>
```

A flat mid-grey backdrop rather than a photograph is deliberate: a busy wallpaper
is noise in every screenshot the model is charged for, and it lowers the contrast
of anything drawn over it.

**The panel first-run dialog.** `xfce4-panel`'s migration step reads the
environment before deciding whether to ask:

> `if (g_getenv ("XFCE_PANEL_MIGRATE_DEFAULT") != NULL || migrate_vendor_default) goto migrate_default;`
> — [`migrate/main.c`](https://github.com/xfce-mirror/xfce4-panel/blob/master/migrate/main.c)

So `ENV XFCE_PANEL_MIGRATE_DEFAULT=1` in the Dockerfile is the fix, and it is
verified rather than folklore. Preseeding `xfce4-panel.xml` also works because
the dialog only appears when the channel has no `/panels` property at all.

**Screen blanking and DPMS.** X's own blanking is independent of any screensaver
package, so purging `xfce4-screensaver` is not enough. Add to `entrypoint.sh`,
after Xvfb is up (E2B does the same thing via an autostart `.desktop` file —
doing it in the entrypoint is more deterministic):

```bash
xset -display "${DISPLAY}" s off s noblank -dpms
```

**Notifications.** `xfce4-notifyd` is purged in §2. If something re-introduces a
notification daemon, `/etc/xdg/xfce4/xfconf/xfce-perchannel-xml/xfce4-notifyd.xml`
with `/notification-log/enabled=false` and `/expire-timeout=1` limits the damage
(**unverified** key names).

**GTK.** `/etc/xdg/gtk-3.0/settings.ini`:

```ini
[Settings]
gtk-enable-animations=0
gtk-recent-files-max-age=0
gtk-recent-files-limit=0
gtk-primary-button-warps-slider=false
gtk-dialogs-use-header=false
gtk-overlay-scrolling=false
```

`gtk-overlay-scrolling=false` gives every GTK app a permanently visible
scrollbar, which is a real grounding target instead of one that appears only on
hover. `gtk-enable-animations=0` removes the window in which a screenshot catches
a half-drawn widget — which is a direct cause of our dHash stall detector firing
on a screen that is genuinely mid-transition.

**A manifest for the prompt.** Write `/etc/agentfleet/manifest.json` in the build
listing each installed application, its launch command and one line of purpose,
and have `agentd` serve it from `/health`. `AGENT-HARNESS.md` §3.8 uses it to
tell the model what exists — today the model is told its vCPU count and not that
Firefox is installed.

---

## 9. Size budget

Approximate installed sizes. Verified figures are marked; the rest are estimates
and should be confirmed with `apt-get install -s` before anyone plans around them.

| Group | Size |
|---|---|
| Ubuntu 24.04 base | ~80 MB |
| Xvfb + x11vnc + noVNC + websockify + supervisor | ~90 MB |
| XFCE (four components, not the metapackage) | ~180 MB |
| at-spi2-core, python3-gi, pyatspi, D-Bus | ~40 MB |
| xdotool, wmctrl, xclip, scrot, imagemagick | ~90 MB |
| agentd venv (fastapi, uvicorn, pydantic, Pillow, mss, python-xlib) | ~120 MB |
| Fonts (§6) | ~148 MB (CJK 91 MB **verified**) |
| Firefox (Mozilla .deb) | ~250 MB |
| LibreOffice Writer/Calc/Impress + gtk3 | ~300 MB (`libreoffice-core` alone is **148 MB verified**) |
| Thunar, Mousepad, Atril, Ristretto, xarchiver, p7zip | ~70 MB |
| tesseract + eng | ~15 MB |
| **base total** | **≈ 1.4 GB** |
| `-dev` adds: build-essential, cmake, ninja, git, JDK, node, ffmpeg, X/GL headers, gdb | **≈ +1.4 GB** |
| **`-dev` total** | **≈ 2.8 GB** |

For context, published Godot CI images alone are 956 MB – 1.39 GB
([Docker Hub](https://hub.docker.com/r/robpc/godot-headless)), which is the whole
base image again — further evidence for fetching engines on demand.

`fleet/tiers.go` already points `developer-heavy` at `image + "-dev"`, so the
split needs no orchestrator change, only a second Dockerfile stage. Build the
base with `--squash`-equivalent layer discipline: the apt lists removal is
already correct, and the `-dev` layer must not `apt-get update` without a
matching `rm -rf /var/lib/apt/lists/*`.

---

## 10. Ranked changes

1. **Add LibreOffice (Writer/Calc/Impress + gtk3) and set `SAL_USE_VCLPLUGIN=gtk3`.**
   31.6% of the benchmark workload is currently impossible in our image.
2. **`accessibility.force_disabled = -1` plus the Firefox policy and autoconfig
   files.** Makes the browser's a11y tree deterministic instead of race-dependent,
   and removes every first-run modal in one layer.
3. **Fonts: `fonts-noto-core`, `fonts-noto-cjk`, `fonts-noto-color-emoji` and the
   fontconfig rule.** Tofu is silent corruption of the model's input.
4. **Drop the `xfce4` metapackage; preseed the xfconf channels; set
   `XFCE_PANEL_MIGRATE_DEFAULT=1`; `xset s off -dpms` in the entrypoint.**
   Removes every recurring dialog and one workspace-switch failure mode.
5. **Add Thunar, Mousepad, Atril, Ristretto, archive tools and tesseract.** File
   management is 6.5% of tasks on its own and threaded through the 27.6% that are
   multi-app; `atril` over `xpdf` is specifically an accessibility decision.

---

## 11. Sources

- OSWorld task inventory — https://github.com/xlang-ai/OSWorld/tree/main/evaluation_examples/examples · paper https://arxiv.org/pdf/2404.07972
- Anthropic computer-use demo Dockerfile — https://github.com/anthropics/claude-quickstarts/blob/main/computer-use-demo/Dockerfile
- E2B desktop template — https://github.com/e2b-dev/desktop/blob/main/template/template.py · Firefox cfg https://github.com/e2b-dev/desktop/blob/main/template/files/firefox.cfg
- Mozilla policy templates — https://mozilla.github.io/policy-templates/
- Firefox `accessibility.force_disabled` semantics — https://github.com/mozilla-firefox/firefox/blob/main/accessible/base/nsAccessibilityService.cpp
- xfce4-panel first-run migration — https://github.com/xfce-mirror/xfce4-panel/blob/master/migrate/main.c
- `fonts-noto-cjk` size — https://packages.ubuntu.com/noble/fonts-noto-cjk
- `libreoffice-core` size — https://packages.ubuntu.com/noble/libreoffice-core
- Godot CI image sizes — https://hub.docker.com/r/robpc/godot-headless · https://hub.docker.com/r/barichello/godot-ci
- Unity licensing in containers — https://issuetracker.unity3d.com/issues/cannot-activate-license-within-a-docker-container · https://game.ci/docs/gitlab/activation/
- Unreal container requirements — https://dev.epicgames.com/documentation/unreal-engine/building-the-linux-container-images-from-source
