import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_tts/flutter_tts.dart';
import 'package:just_audio/just_audio.dart';
import 'package:speech_to_text/speech_to_text.dart';

import '../network/api_client.dart';

/// Speech in and out, on device.
///
/// Deliberately not a server round trip. The orchestrator has no STT or TTS
/// provider, and the sandbox's "speak" action synthesises three summed sine
/// waves rather than words — so routing audio through the backend would buy
/// latency and a hum. Android and iOS both ship a recogniser and a synthesiser;
/// using them keeps the audio on the handset and works offline on most devices.
///
/// Voice is a way to talk to an agent, so this is a plain service used by the
/// agent's chat screen rather than a destination of its own.
class VoiceService {
  VoiceService({SpeechToText? speech, FlutterTts? tts, ApiClient? api})
      : _speech = speech ?? SpeechToText(),
        _tts = tts ?? FlutterTts(),
        _api = api;

  final SpeechToText _speech;
  final FlutterTts _tts;

  /// When set, speech is rendered by the server's TTS service — real voices,
  /// identical on every device. Without it, or if that call fails, the phone's
  /// own synthesiser is used instead: worse voices, but it always works and
  /// needs no network.
  final ApiClient? _api;
  final AudioPlayer _player = AudioPlayer();

  bool _ready = false;
  bool _unavailable = false;
  String _lastError = '';

  /// Words-per-minute feel. flutter_tts takes 0.0-1.0 on Android where 0.5 is
  /// normal, so this is stored as the platform value rather than a multiplier
  /// to avoid a lossy round trip through the settings UI.
  double _rate = 0.5;
  double _pitch = 1.0;
  String? _voiceName;

  /// Server voice id, or null to use the device.
  String? _serverVoice;
  double _serverSpeed = 1.0;

  bool get isListening => _speech.isListening;

  /// Why voice is unusable, or empty when it is fine. Surfaced verbatim so a
  /// denied microphone permission does not present as a silent no-op — which
  /// is exactly how the previous mock behaved.
  String get lastError => _lastError;
  bool get unavailable => _unavailable;

  /// Idempotent: safe to call before every listen.
  Future<bool> init() async {
    if (_ready) return true;
    if (_unavailable) return false;
    try {
      _ready = await _speech.initialize(
        onError: (e) => _lastError = e.errorMsg,
        onStatus: (_) {},
      );
      if (!_ready) {
        _unavailable = true;
        if (_lastError.isEmpty) {
          _lastError = 'speech recognition is unavailable on this device';
        }
      }
    } catch (e) {
      _ready = false;
      _unavailable = true;
      _lastError = '$e';
    }
    return _ready;
  }

  /// Listens until the speaker stops, then hands back the final transcript.
  ///
  /// [onPartial] fires as words are recognised so the UI can show progress;
  /// without it a long utterance looks like the app has hung.
  Future<String?> listenOnce({
    ValueChanged<String>? onPartial,
    Duration limit = const Duration(seconds: 30),
    Duration pauseFor = const Duration(seconds: 3),
  }) async {
    if (!await init()) return null;

    final done = Completer<String?>();
    var best = '';

    await _speech.listen(
      onResult: (r) {
        best = r.recognizedWords;
        onPartial?.call(best);
        if (r.finalResult && !done.isCompleted) {
          done.complete(best.trim().isEmpty ? null : best.trim());
        }
      },
      listenOptions: SpeechListenOptions(
        partialResults: true,
        cancelOnError: true,
        listenFor: limit,
        pauseFor: pauseFor,
      ),
    );

    // The plugin does not always deliver a final result — stopping early, or a
    // recogniser that just goes quiet. Falling back to the best partial means
    // the user's words are not silently dropped.
    unawaited(Future.delayed(limit + const Duration(seconds: 1), () {
      if (!done.isCompleted) {
        done.complete(best.trim().isEmpty ? null : best.trim());
      }
    }));

    return done.future;
  }

  Future<void> stopListening() async {
    if (_speech.isListening) await _speech.stop();
  }

  /// Reads a reply aloud. Failures are swallowed: not speaking is a degraded
  /// experience, but throwing here would break the chat that produced the text.
  /// Available system voices, as {name, locale} maps. Empty on a device with
  /// no synthesiser installed.
  Future<List<Map<String, String>>> voices() async {
    try {
      final raw = await _tts.getVoices as List?;
      return (raw ?? [])
          .map((v) => (v as Map).map((k, val) => MapEntry('$k', '$val')))
          .where((v) => (v['name'] ?? '').isNotEmpty)
          .toList();
    } catch (e) {
      _lastError = '$e';
      return const [];
    }
  }

  /// Applied before every utterance rather than once at startup: the engine
  /// resets between speakers on some Android builds, and a rate that silently
  /// reverts is worse than one that never changed.
  /// Choose the server voice. Null returns to the device's own synthesiser.
  void useServerVoice(String? voiceId, {double speed = 1.0}) {
    _serverVoice = (voiceId == null || voiceId.isEmpty) ? null : voiceId;
    _serverSpeed = speed;
  }

  Future<void> configure({double? rate, double? pitch, String? voiceName}) async {
    if (rate != null) _rate = rate.clamp(0.1, 1.0);
    if (pitch != null) _pitch = pitch.clamp(0.5, 2.0);
    if (voiceName != null) _voiceName = voiceName.isEmpty ? null : voiceName;
  }

  Future<void> _applySettings() async {
    await _tts.setSpeechRate(_rate);
    await _tts.setPitch(_pitch);
    final name = _voiceName;
    if (name != null) {
      // setVoice needs the locale too; look the chosen name back up so a
      // stored preference keeps working across reboots.
      for (final v in await voices()) {
        if (v['name'] == name) {
          await _tts.setVoice({'name': name, 'locale': v['locale'] ?? ''});
          break;
        }
      }
    }
  }

  Future<void> speak(String text) async {
    final trimmed = text.trim();
    if (trimmed.isEmpty) return;

    if (_api != null && _serverVoice != null) {
      try {
        await _speakViaServer(trimmed);
        return;
      } catch (e) {
        // Falling through to the device is the right failure: the operator
        // hears the reply in a worse voice rather than hearing nothing and
        // wondering whether the agent answered at all.
        _lastError = 'server speech failed, used the device voice: $e';
      }
    }

    try {
      await _tts.stop();
      await _applySettings();
      await _tts.speak(trimmed);
    } catch (e) {
      _lastError = '$e';
    }
  }

  Future<void> _speakViaServer(String text) async {
    final bytes = await _api!.speak(text, voice: _serverVoice, speed: _serverSpeed);
    if (bytes.isEmpty) throw StateError('empty audio');
    await _player.stop();
    // Fed as bytes rather than a URL so playback needs no second authenticated
    // request from the audio stack.
    await _player.setAudioSource(_WavSource(Uint8List.fromList(bytes)));
    await _player.play();
  }

  Future<void> stopSpeaking() async {
    try {
      await _tts.stop();
    } catch (_) {}
    try {
      await _player.stop();
    } catch (_) {}
  }

  Future<void> dispose() async {
    await stopListening();
    await stopSpeaking();
    await _player.dispose();
  }
}

/// Plays WAV bytes already in memory.
///
// ignore_for_file: experimental_member_use
// StreamAudioResponse is how just_audio exposes an in-memory source; there is
// no stable alternative, and the whole point is to avoid a second
// authenticated fetch from the audio stack.
class _WavSource extends StreamAudioSource {
  _WavSource(this._bytes);
  final Uint8List _bytes;

  @override
  Future<StreamAudioResponse> request([int? start, int? end]) async {
    start ??= 0;
    end ??= _bytes.length;
    return StreamAudioResponse(
      sourceLength: _bytes.length,
      contentLength: end - start,
      offset: start,
      stream: Stream.value(_bytes.sublist(start, end)),
      contentType: 'audio/wav',
    );
  }
}
