#!/usr/bin/env python3
"""Example 3: synthesize speech with the Pocket TTS sidecar.

`speak()` returns the WAV bytes — this writes one file per voice so you have
something to actually listen to. The sidecar is optional; a fleet without one
says so and this example reports that instead of pretending it spoke. (An
earlier version printed a success line for every voice against an endpoint the
SDK could never reach.)
"""

from pathlib import Path

from agentfleet import FleetClient
from agentfleet.exceptions import FleetApiError

VOICES = [
    ("shadow", "Male (default) — low and level"),
    ("atlas", "Male — warm, unhurried"),
    ("vortex", "Male — bright and quick"),
    ("echo", "Male — clear, neutral"),
    ("aura", "Female — soft, higher register"),
    ("lyra", "Female — light and articulate"),
]


def main():
    fleet = FleetClient("http://localhost:8080")
    # fleet.login("you@example.com", "...")   # or FleetClient(token=...)

    # Ask first rather than failing six times: availability is reported, and a
    # deployment with no TTS sidecar is a normal configuration.
    catalogue = fleet.voices()
    if not catalogue.get("available"):
        print("🔇 No text-to-speech sidecar on this fleet:")
        print(f"   {catalogue.get('reason', 'not deployed')}")
        print("   Deploy the `tts` compose service and re-run.")
        return

    out = Path("tts-samples")
    out.mkdir(exist_ok=True)

    print("🎙️ Synthesizing the six curated voices:\n")
    for vid, blurb in VOICES:
        try:
            wav = fleet.speak(f"OpenAgentFleet voice profile {vid} operational.", voice=vid)
        except FleetApiError as exc:
            print(f"  ✗ {vid}: {exc}")
            continue
        path = out / f"{vid}.wav"
        path.write_bytes(wav)
        print(f"  ✓ {vid:<7} {len(wav):>7,} bytes -> {path}   ({blurb})")

    print(f"\nPlay them from {out}/ — every agent on the fleet speaks with these.")


if __name__ == "__main__":
    main()
