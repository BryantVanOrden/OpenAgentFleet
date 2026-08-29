# Flutter Cross-Platform Companion Guide 📱💻

This comprehensive guide covers building and connecting the **AgentFleet Flutter Companion App** across **Linux Desktop**, **Windows Desktop**, **Apple macOS & iOS**, and **Android**.

---

## 🌐 1. Architecture & Connection Overview

The Flutter companion app is designed for triage, real-time agent monitoring, multi-bot DAG execution, duplex voice co-pilot dialogue, and human-in-the-loop alert resolution.

```
 ┌────────────────────────────────────────────────────────┐
 │         Flutter Companion (Cross-Platform)             │
 │  (Android · iOS · macOS · Linux Desktop · Windows EXE) │
 └──────────────┬─────────────────────────┬───────────────┘
                │ HTTP REST               │ WebSocket
                │ (/api/*)                │ (/api/events)
 ┌──────────────▼─────────────────────────▼───────────────┐
 │               AgentFleet Orchestrator                  │
 │               (Default: Port 8080)                     │
 └────────────────────────────────────────────────────────┘
```

When you first launch the app on any platform, navigate to **Settings (⚙️)** to configure your orchestrator endpoint:
* **Local Machine (Desktop apps on same machine as Docker)**: `http://localhost:8080`
* **Local Network (Phone / Tablet on same Wi-Fi)**: `http://192.168.1.X:8080` (your host machine's LAN IP)
* **Secure Remote Access (Anywhere on the go)**: `https://fleet.yourdomain.com` or `http://100.X.Y.Z:8080` (Tailscale VPN IP)

---

## 🐧 2. Linux Desktop Runner

### A. System Prerequisites
On Ubuntu / Debian / Linux Mint:
```bash
sudo apt update
sudo apt install -y clang cmake ninja-build pkg-config libgtk-3-dev liblzma-dev libsecret-1-dev
```

On Fedora / RHEL:
```bash
sudo dnf install -y clang cmake ninja-build pkgconf-pkg-config gtk3-devel libsecret-devel xz-devel
```

On Arch Linux:
```bash
sudo pacman -S --needed clang cmake ninja pkgconf gtk3 libsecret xz
```

### B. Build & Run
```bash
cd mobile

# 1. Enable Linux desktop target in Flutter
flutter config --enable-linux-desktop

# 2. Get dependencies
flutter pub get

# 3. Run in debug mode (with hot reload)
flutter run -d linux

# 4. Or compile a standalone release bundle
flutter build linux --release
```

The release binary will be located at:
`mobile/build/linux/x64/release/bundle/agentfleet_companion`

---

## 🪟 3. Windows Desktop Runner (.exe)

### A. System Prerequisites
1. Install **Visual Studio 2022** (Community or higher).
2. During installation, check the workload: **"Desktop development with C++"**.
3. Verify that the **MSVC v143 toolset** and **Windows 10/11 SDK** are selected.
4. Verify with `flutter doctor` that the Windows toolchain is detected with a green checkmark (`[✓]`).

### B. Build & Run
```powershell
cd mobile

# 1. Enable Windows desktop target
flutter config --enable-windows-desktop

# 2. Fetch packages
flutter pub get

# 3. Run in debug mode
flutter run -d windows

# 4. Compile standalone release executable
flutter build windows --release
```

The release executable and required DLL dependencies will be located at:
`mobile\build\windows\x64\runner\Release\agentfleet_companion.exe`

---

## 🍎 4. Apple Runners (macOS Desktop & iOS / iPadOS)

### A. Prerequisites
* **macOS Machine** running macOS Sonoma or Sequoia.
* **Xcode 15+** installed from the Mac App Store.
* **CocoaPods**:
  ```bash
  sudo gem install cocoapods
  ```

---

### B. macOS Native Desktop Build
```bash
cd mobile

# 1. Enable macOS desktop target
flutter config --enable-macos-desktop

# 2. Install CocoaPods dependencies
cd macos && pod install && cd ..

# 3. Run macOS desktop application
flutter run -d macos

# 4. Compile release application bundle
flutter build macos --release
```
The compiled macOS app bundle will be located at:
`mobile/build/macos/Build/Products/Release/agentfleet_companion.app`

---

### C. iOS Device & Simulator Build
```bash
cd mobile

# 1. Install iOS CocoaPods
cd ios && pod install && cd ..

# 2. Run on connected iPhone or iOS Simulator
flutter run -d iphone

# 3. Build release IPA for TestFlight or Ad-Hoc distribution
flutter build ipa --release --no-codesign
```

> [!NOTE]
> When testing on a physical iPhone over local Wi-Fi, ensure your iOS device and host computer are on the same subnet, and enter your computer's LAN IP (e.g. `http://192.168.1.150:8080`) in the app's Settings screen.

---

## 🤖 5. Android Mobile Runner (APK & USB Debugging)

### A. Run via ADB Debugging
```bash
cd mobile
flutter run -d android
```

### B. Build Production Release APKs
```bash
cd mobile
flutter build apk --release --split-per-abi
```
Generated APKs:
* ARM64 (Modern smartphones): `mobile/build/app/outputs/flutter-apk/app-arm64-v8a-release.apk`
* x86_64 (Emulators / Chromebooks): `mobile/build/app/outputs/flutter-apk/app-x86_64-release.apk`

---

## 🔒 6. Remote Connectivity Best Practices (On-The-Go Access)

To connect your phone or laptop companion app to your AgentFleet orchestrator from outside your home/office network:

### Option A: Tailscale Private Mesh VPN (Recommended)
1. Install [Tailscale](https://tailscale.com) on your host server running AgentFleet and on your phone.
2. Open the Flutter Companion App on your phone.
3. In **Settings**, set the API Endpoint to your host server's Tailscale IP:
   `http://100.X.Y.Z:8080`
4. Enjoy end-to-end encrypted zero-trust access with **zero open router ports**.

### Option B: Reverse Proxy with HTTPS (Cloudflare Tunnel / Caddy / Nginx)
1. Route incoming HTTPS traffic to `localhost:8080`.
2. Ensure WebSocket upgrades are enabled for `/api/events`.
3. In the Flutter App, enter: `https://fleet.yourdomain.com`.

---

## 📊 Platform Feature Support Matrix

| Feature | Android | iOS | macOS | Linux Desktop | Windows EXE |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **Fleet Monitoring & CPU/RAM Meters** | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Duplex Voice Co-Pilot (Pocket TTS)** | ✅ | ✅ | ✅ | ✅ | ✅ |
| **P2P Comms & Shared Secret Manager** | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Multi-Bot DAG Pipeline Dispatcher** | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Live Single-Frame Desktop Viewer** | ✅ | ✅ | ✅ | ✅ | ✅ |
| **FCM / APNs Push Notifications** | ✅ | ✅ | ✖ | ✖ | ✖ |
| **Interactive noVNC WebView Takeover**| ✅ | ✅ | ✅ | ✅ (CEF) | ✅ (CEF) |
