import io
import unittest
import wave
from voice import VOICE, DEFAULT_VOICE, VOICE_PROFILES

class TestVoiceEngine(unittest.TestCase):
    def test_voice_profiles_count_and_default(self):
        self.assertEqual(len(VOICE_PROFILES), 6)
        self.assertEqual(DEFAULT_VOICE, "shadow")

        voices = VOICE.list_voices()
        self.assertEqual(len(voices), 6)
        
        male_count = sum(1 for v in voices if v["gender"] == "male")
        female_count = sum(1 for v in voices if v["gender"] == "female")
        self.assertEqual(male_count, 4, "Must have exactly 4 male voices")
        self.assertEqual(female_count, 2, "Must have exactly 2 female voices")

        default_v = next(v for v in voices if v["is_default"])
        self.assertEqual(default_v["id"], "shadow")
        self.assertEqual(default_v["gender"], "male")

    def test_synthesize_wav_output(self):
        audio_bytes = VOICE.synthesize_wav("System operational. Task started.", voice="shadow")
        self.assertTrue(len(audio_bytes) > 100)
        
        # Verify valid WAV format header
        with wave.open(io.BytesIO(audio_bytes), "rb") as wf:
            self.assertEqual(wf.getnchannels(), 1)
            self.assertEqual(wf.getsampwidth(), 2)
            self.assertEqual(wf.getframerate(), 22050)
            self.assertTrue(wf.getnframes() > 0)

    def test_female_voice_synthesis(self):
        audio_bytes = VOICE.synthesize_wav("Aura voice test.", voice="aura")
        self.assertTrue(len(audio_bytes) > 100)
        with wave.open(io.BytesIO(audio_bytes), "rb") as wf:
            self.assertTrue(wf.getnframes() > 0)

if __name__ == "__main__":
    unittest.main()
