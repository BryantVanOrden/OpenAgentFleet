#!/usr/bin/env python3
"""Example 3: Pocket TTS Real-Time Voice Synthesis across 6 curated voice models."""

from agentfleet import FleetClient

def main():
    fleet = FleetClient("http://localhost:8080")

    # 4 Male voices, 2 Female voices
    voices = [
        ("shadow", "Male (Default)", "Cyberpunk operative & tech lead"),
        ("atlas", "Male", "Resonant, authoritative architectural leader"),
        ("vortex", "Male", "Dynamic, high-velocity engineer"),
        ("echo", "Male", "Calm, analytical quant"),
        ("aura", "Female", "Crisp, futuristic AI co-pilot"),
        ("lyra", "Female", "Warm, conversational guide"),
    ]

    print("🎙️ Testing Pocket TTS Curated Voice Models:\n")
    for vid, gender, tag in voices:
        text = f"AgentFleet voice profile {vid} operational. Standing by for task instructions."
        print(f"Synthesizing [{vid.upper()}] ({gender} - {tag})...")
        res = fleet.speak(text, voice=vid)
        print(f" -> Output voice: {res.get('voice', vid)}\n")

if __name__ == "__main__":
    main()
