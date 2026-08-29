import 'package:shared_preferences/shared_preferences.dart';

import 'voice_service.dart';

const _kRateKey = 'voice.rate';
const _kPitchKey = 'voice.pitch';
const _kVoiceKey = 'voice.name';

/// Delivery preferences for speech — how fast and how high, not who.
///
/// Which voice an agent speaks in is a property of that agent and lives in its
/// own settings, so there is no app-wide voice picker to read here. These are
/// the playback defaults that apply whoever is talking.
class VoicePrefs {
  const VoicePrefs(
      {required this.rate, required this.pitch, required this.voiceName});

  final double rate;
  final double pitch;

  /// Device-TTS voice, used only as a fallback when an agent has no server
  /// voice set and we are speaking through the platform engine.
  final String voiceName;

  static VoicePrefs read(SharedPreferences p) => VoicePrefs(
        // 0.5 is the platform's "normal" on Android, not 1.0.
        rate: p.getDouble(_kRateKey) ?? 0.5,
        pitch: p.getDouble(_kPitchKey) ?? 1.0,
        voiceName: p.getString(_kVoiceKey) ?? '',
      );
}

/// Push stored preferences into a service before it speaks.
Future<void> applyStoredVoiceSettings(
    VoiceService voice, SharedPreferences prefs) async {
  final p = VoicePrefs.read(prefs);
  await voice.configure(rate: p.rate, pitch: p.pitch, voiceName: p.voiceName);
}
