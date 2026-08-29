import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../core/theme/theme_controller.dart';
import '../../core/theme/theme.dart';
import '../../core/voice/voice_service.dart';

const _kRateKey = 'voice.rate';
const _kPitchKey = 'voice.pitch';
const _kVoiceKey = 'voice.name';

/// Voice preferences, held in SharedPreferences so a chosen voice survives a
/// restart. Read by [applyStoredVoiceSettings] wherever a VoiceService is about
/// to speak.
class VoicePrefs {
  const VoicePrefs({required this.rate, required this.pitch, required this.voiceName});

  final double rate;
  final double pitch;
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

class VoiceSettingsCard extends ConsumerStatefulWidget {
  const VoiceSettingsCard({super.key});

  @override
  ConsumerState<VoiceSettingsCard> createState() => _VoiceSettingsCardState();
}

class _VoiceSettingsCardState extends ConsumerState<VoiceSettingsCard> {
  final _voice = VoiceService();

  double _rate = 0.5;
  double _pitch = 1.0;
  String _voiceName = '';
  List<Map<String, String>> _voices = const [];
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _voice.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final prefs = ref.read(sharedPreferencesProvider);
    final stored = VoicePrefs.read(prefs);
    final list = await _voice.voices();
    if (!mounted) return;
    setState(() {
      _rate = stored.rate;
      _pitch = stored.pitch;
      _voiceName = stored.voiceName;
      // Device voices are noisy — dozens of near-identical locale variants.
      // Sorting keeps the list navigable without hiding anything.
      _voices = list..sort((a, b) => (a['name'] ?? '').compareTo(b['name'] ?? ''));
      _loading = false;
    });
  }

  Future<void> _save() async {
    final prefs = ref.read(sharedPreferencesProvider);
    await prefs.setDouble(_kRateKey, _rate);
    await prefs.setDouble(_kPitchKey, _pitch);
    await prefs.setString(_kVoiceKey, _voiceName);
    await _voice.configure(rate: _rate, pitch: _pitch, voiceName: _voiceName);
  }

  Future<void> _preview() async {
    await _save();
    await _voice.speak(
        'This is how the agent will sound when it reads a reply back to you.');
  }

  @override
  Widget build(BuildContext context) {
    return Card(
      color: Fleet.ink850,
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.record_voice_over_outlined, size: 18),
                const SizedBox(width: 8),
                Text('Voice', style: Theme.of(context).textTheme.titleSmall),
              ],
            ),
            const SizedBox(height: 4),
            Text(
              'Used when an agent reads a reply aloud in chat. Speech is '
              'produced on this phone, so it works offline and no audio is '
              'sent to the server.',
              style: TextStyle(color: Fleet.ink400, fontSize: 12, height: 1.35),
            ),
            const SizedBox(height: 12),
            if (_loading)
              const LinearProgressIndicator(minHeight: 2)
            else ...[
              if (_voices.isEmpty)
                Text(
                  'No speech voices are installed on this device, so replies '
                  'cannot be read aloud.',
                  style: TextStyle(color: Fleet.warn, fontSize: 12),
                )
              else
                DropdownButtonFormField<String>(
                  initialValue: _voices.any((v) => v['name'] == _voiceName)
                      ? _voiceName
                      : '',
                  isExpanded: true,
                  decoration: const InputDecoration(labelText: 'Voice'),
                  items: [
                    const DropdownMenuItem(
                        value: '', child: Text('System default')),
                    for (final v in _voices)
                      DropdownMenuItem(
                        value: v['name'],
                        child: Text(
                          '${v['name']}${(v['locale'] ?? '').isEmpty ? '' : '  ·  ${v['locale']}'}',
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                  ],
                  onChanged: (v) {
                    setState(() => _voiceName = v ?? '');
                    _save();
                  },
                ),
              const SizedBox(height: 8),
              _Slider(
                label: 'Speed',
                value: _rate,
                min: 0.1,
                max: 1.0,
                // Android treats 0.5 as normal, so show it relative to that
                // rather than as a bare number nobody can interpret.
                display: '${(_rate / 0.5).toStringAsFixed(2)}x',
                onChanged: (v) => setState(() => _rate = v),
                onSettled: _save,
              ),
              _Slider(
                label: 'Pitch',
                value: _pitch,
                min: 0.5,
                max: 2.0,
                display: _pitch.toStringAsFixed(2),
                onChanged: (v) => setState(() => _pitch = v),
                onSettled: _save,
              ),
              const SizedBox(height: 4),
              Align(
                alignment: Alignment.centerLeft,
                child: OutlinedButton.icon(
                  onPressed: _voices.isEmpty ? null : _preview,
                  icon: const Icon(Icons.play_arrow_rounded, size: 18),
                  label: const Text('Preview'),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _Slider extends StatelessWidget {
  const _Slider({
    required this.label,
    required this.value,
    required this.min,
    required this.max,
    required this.display,
    required this.onChanged,
    required this.onSettled,
  });

  final String label;
  final double value;
  final double min;
  final double max;
  final String display;
  final ValueChanged<double> onChanged;
  final VoidCallback onSettled;

  @override
  Widget build(BuildContext context) => Row(
        children: [
          SizedBox(
            width: 54,
            child: Text(label, style: const TextStyle(fontSize: 12)),
          ),
          Expanded(
            child: Slider(
              value: value.clamp(min, max),
              min: min,
              max: max,
              onChanged: onChanged,
              // Saving on every frame of a drag would hammer preferences;
              // once the thumb settles is enough.
              onChangeEnd: (_) => onSettled(),
            ),
          ),
          SizedBox(
            width: 44,
            child: Text(display,
                textAlign: TextAlign.end,
                style: TextStyle(color: Fleet.ink300, fontSize: 11)),
          ),
        ],
      );
}
