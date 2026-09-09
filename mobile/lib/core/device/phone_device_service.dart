import 'dart:async';
import 'dart:io' show Platform;

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:url_launcher/url_launcher.dart';

import '../models.dart';
import '../network/api_client.dart';
import '../voice/voice_service.dart';

/// This phone as a device Oaf can act on.
///
/// Registers the phone, then polls for jobs while the app is open and runs
/// the ones a phone can do: show a notification, open a link, read or set the
/// clipboard, say something out loud. A phone exposes no folders, so the file
/// and shell tools answer "not on a phone" rather than pretending; those are
/// what `fleetctl host` on a PC is for.
///
/// Switched on from Settings and remembered; the device id is kept so a
/// session bound to this phone stays bound across restarts.
class PhoneDeviceService extends ChangeNotifier {
  PhoneDeviceService(this._api, this._voice);

  static const _enabledKey = 'oaf.phone.enabled';
  static const _idKey = 'oaf.phone.device_id';
  static const _nameKey = 'oaf.phone.name';

  final ApiClient _api;
  final VoiceService _voice;
  final _notifications = FlutterLocalNotificationsPlugin();

  bool _enabled = false;
  bool _running = false;
  String _deviceId = '';
  String _name = '';
  String _status = 'off';
  int _handled = 0;
  bool _notificationsReady = false;

  bool get enabled => _enabled;
  bool get running => _running;
  String get deviceId => _deviceId;
  String get name => _name;
  String get status => _status;
  int get handled => _handled;

  Future<void> load() async {
    final prefs = await SharedPreferences.getInstance();
    _enabled = prefs.getBool(_enabledKey) ?? false;
    _deviceId = prefs.getString(_idKey) ?? '';
    _name = prefs.getString(_nameKey) ?? _defaultName();
    notifyListeners();
    if (_enabled && _api.isConfigured) unawaited(start());
  }

  String _defaultName() {
    if (kIsWeb) return 'This browser';
    try {
      return Platform.isIOS ? 'This iPhone' : 'This phone';
    } catch (_) {
      return 'This phone';
    }
  }

  String _platform() {
    if (kIsWeb) return 'web';
    try {
      return '${Platform.operatingSystem} ${Platform.operatingSystemVersion}';
    } catch (_) {
      return 'unknown';
    }
  }

  Future<void> setEnabled(bool on, {String? name}) async {
    final prefs = await SharedPreferences.getInstance();
    _enabled = on;
    if (name != null && name.trim().isNotEmpty) {
      _name = name.trim();
      await prefs.setString(_nameKey, _name);
    }
    await prefs.setBool(_enabledKey, on);
    notifyListeners();
    if (on) {
      await start();
    } else {
      await stop();
    }
  }

  Future<void> start() async {
    if (_running) return;
    _running = true;
    _status = 'registering';
    notifyListeners();
    try {
      final dev = await _api.registerOafDevice(
        id: _deviceId,
        name: _name,
        platform: _platform(),
        roots: const [],
        autoApprove: true,
      );
      _deviceId = dev.id;
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(_idKey, _deviceId);
      _status = 'online';
      notifyListeners();
      unawaited(_loop());
    } catch (err) {
      _status = 'failed: $err';
      _running = false;
      notifyListeners();
    }
  }

  Future<void> stop() async {
    _running = false;
    _status = 'off';
    notifyListeners();
  }

  Future<void> _loop() async {
    var backoff = const Duration(seconds: 2);
    while (_running) {
      List<DeviceJob> jobs;
      try {
        jobs = await _api.pollDeviceJobs(_deviceId);
        backoff = const Duration(seconds: 2);
        if (_status != 'online') {
          _status = 'online';
          notifyListeners();
        }
      } catch (err) {
        _status = 'reconnecting';
        notifyListeners();
        await Future<void>.delayed(backoff);
        if (backoff < const Duration(minutes: 1)) backoff *= 2;
        continue;
      }
      for (final job in jobs) {
        if (!_running) break;
        await _handle(job);
      }
    }
  }

  Future<void> _handle(DeviceJob job) async {
    String state;
    String result = '';
    String error = '';
    try {
      result = await _execute(job);
      state = 'done';
    } on UnsupportedError catch (err) {
      state = 'failed';
      error = err.message ?? 'not supported on a phone';
    } catch (err) {
      state = 'failed';
      error = '$err';
    }
    _handled++;
    notifyListeners();
    try {
      await _api.finishDeviceJob(_deviceId, job.id, state: state, result: result, error: error);
    } catch (_) {
      // The orchestrator will time the job out; nothing more to do here.
    }
  }

  Future<String> _execute(DeviceJob job) async {
    final args = job.args;
    switch (job.kind) {
      case 'notify':
        final text = (args['text'] ?? '').toString();
        await _notify(text);
        return 'shown on the phone';
      case 'open_url':
        final raw = (args['url'] ?? '').toString();
        final uri = Uri.tryParse(raw);
        if (uri == null || !(uri.scheme == 'http' || uri.scheme == 'https')) {
          throw ArgumentError('open_url needs an http(s) url');
        }
        if (!await launchUrl(uri, mode: LaunchMode.externalApplication)) {
          throw StateError('could not open $raw');
        }
        return 'opened $raw';
      case 'clipboard':
        final set = args['text'];
        if (set != null) {
          await Clipboard.setData(ClipboardData(text: set.toString()));
          return 'copied ${set.toString().length} chars to the clipboard';
        }
        final data = await Clipboard.getData(Clipboard.kTextPlain);
        return data?.text ?? '(clipboard is empty)';
      case 'speak':
        final text = (args['text'] ?? '').toString();
        await _voice.speak(text);
        return 'spoken';
      case 'shell':
      case 'read_file':
      case 'write_file':
      case 'list_dir':
      case 'search':
        throw UnsupportedError('a phone exposes no folders or shell; attach a PC with fleetctl host for that');
      case 'screenshot':
        throw UnsupportedError('a phone cannot screenshot itself for Oaf');
    }
    throw UnsupportedError('unknown job kind ${job.kind}');
  }

  Future<void> _notify(String text) async {
    if (kIsWeb) return;
    if (!_notificationsReady) {
      await _notifications.initialize(
        const InitializationSettings(
          android: AndroidInitializationSettings('@mipmap/ic_launcher'),
          iOS: DarwinInitializationSettings(),
        ),
      );
      _notificationsReady = true;
    }
    await _notifications.show(
      DateTime.now().millisecondsSinceEpoch ~/ 1000 % 100000,
      'Oaf',
      text,
      const NotificationDetails(
        android: AndroidNotificationDetails('oaf', 'Oaf', importance: Importance.high, priority: Priority.high),
        iOS: DarwinNotificationDetails(),
      ),
    );
  }
}
