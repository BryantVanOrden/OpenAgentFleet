import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';

/// Choose the voice a particular agent speaks in.
///
/// Per agent rather than per app: with several running, one shared voice makes
/// the fleet unreadable by ear — you cannot tell who just reported without
/// looking at the screen.
class VoicePicker extends ConsumerStatefulWidget {
  const VoicePicker({super.key, required this.instance});

  final Instance instance;

  static Future<bool?> show(BuildContext context, Instance instance) =>
      showModalBottomSheet<bool>(
        context: context,
        isScrollControlled: true,
        backgroundColor: Fleet.ink900,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
        ),
        builder: (_) => VoicePicker(instance: instance),
      );

  @override
  ConsumerState<VoicePicker> createState() => _VoicePickerState();
}

class _VoicePickerState extends ConsumerState<VoicePicker> {
  String? _selected;

  /// 0 on the instance means "whatever the app is set to". The slider has to
  /// sit somewhere, so it sits at 1.0 and only sends a value once moved —
  /// otherwise opening this sheet would silently pin every bot to a rate
  /// nobody chose.
  late double _speed = widget.instance.voiceSpeed == 0
      ? 1.0
      : widget.instance.voiceSpeed;
  late bool _speedSet = widget.instance.voiceSpeed != 0;

  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _selected = widget.instance.voice;
  }

  Future<void> _save(String voiceId, {bool close = true, double? speedOverride}) async {
    setState(() {
      _selected = voiceId;
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(apiProvider).setInstanceVoice(
            widget.instance.id,
            voiceId,
            // 0 is the server's spelling of "back to the app default". The
            // access route is a partial update, so OMITTING the field keeps
            // the old speed — a reset that sends nothing resets nothing.
            speed: speedOverride ?? (_speedSet ? _speed : null),
          );
      ref.invalidate(instancesProvider);
      if (mounted && close) Navigator.pop(context, true);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final voices = ref.watch(serverVoicesProvider);

    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 18, 20, 18),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                const Icon(Icons.record_voice_over_outlined, size: 20),
                const SizedBox(width: 8),
                Expanded(
                  child: Text('Voice for ${widget.instance.name}',
                      style: Theme.of(context).textTheme.titleMedium),
                ),
              ],
            ),
            const SizedBox(height: 12),
            Flexible(
              child: voices.when(
                loading: () => const Padding(
                  padding: EdgeInsets.all(24),
                  child: Center(child: CircularProgressIndicator()),
                ),
                error: (e, _) => Text('Could not load voices: $e',
                    style: TextStyle(color: Fleet.bad, fontSize: 12)),
                data: (list) {
                  if (list.isEmpty) {
                    // No sidecar deployed. Saying so beats an empty list that
                    // looks like a loading bug.
                    return Text(
                      'The server has no speech service running, so agents '
                      'cannot be given distinct voices. Replies are read aloud '
                      'using this device\'s own voice instead, which you can '
                      'change in Settings.',
                      style: TextStyle(
                          color: Fleet.ink300, fontSize: 13, height: 1.4),
                    );
                  }
                  final presets = list.where((v) => v.preset).toList();
                  final rest = list.where((v) => !v.preset).toList();
                  return ListView(
                    shrinkWrap: true,
                    children: [
                      _tile(const ServerVoice(
                        id: '',
                        name: 'Default',
                        description: 'Whatever the app is set to',
                      )),
                      if (presets.isNotEmpty) _header('Fleet voices'),
                      ...presets.map(_tile),
                      if (rest.isNotEmpty) _header('All voices'),
                      ...rest.map(_tile),
                    ],
                  );
                },
              ),
            ),
            _speedControl(),
            if (_error != null) ...[
              const SizedBox(height: 8),
              Text(_error!, style: TextStyle(color: Fleet.bad, fontSize: 12)),
            ],
          ],
        ),
      ),
    );
  }

  Widget _header(String text) => Padding(
        padding: const EdgeInsets.only(top: 12, bottom: 4),
        child: Text(text.toUpperCase(),
            style: TextStyle(
                color: Fleet.ink400,
                fontSize: 10,
                fontWeight: FontWeight.w700,
                letterSpacing: 0.6)),
      );

  Widget _tile(ServerVoice v) {
    final selected = (_selected ?? '') == v.id;
    return ListTile(
      dense: true,
      contentPadding: EdgeInsets.zero,
      enabled: !_busy,
      leading: Icon(
        selected ? Icons.radio_button_checked : Icons.radio_button_unchecked,
        color: selected ? Fleet.live : Fleet.ink500,
        size: 20,
      ),
      title: Text(v.name, style: const TextStyle(fontSize: 14)),
      subtitle: v.description.isEmpty
          ? null
          : Text(v.description,
              style: TextStyle(color: Fleet.ink400, fontSize: 11)),
      onTap: _busy ? null : () => _save(v.id),
    );
  }

  /// How fast this bot talks.
  ///
  /// The speak endpoint and the speech service always took a rate; there was
  /// simply nowhere to keep one per bot, so every agent spoke at the same
  /// pace. Two agents sharing a voice are still told apart by how fast they
  /// say it, which is the point of giving them distinct voices at all.
  Widget _speedControl() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _header('Speaking speed'),
        Row(
          children: [
            Icon(Icons.slow_motion_video, size: 16, color: Fleet.ink500),
            Expanded(
              child: Slider(
                value: _speed,
                min: 0.5,
                max: 2.0,
                divisions: 15,
                label: _speedSet
                    ? '${_speed.toStringAsFixed(2)}x'
                    : 'default (1.00x)',
                onChanged: _busy
                    ? null
                    : (v) => setState(() {
                          _speed = v;
                          _speedSet = true;
                        }),
                // Saved on release, not on every frame: dragging a slider
                // would otherwise fire a request per pixel.
                onChangeEnd: _busy
                    ? null
                    : (_) => _save(_selected ?? '', close: false),
              ),
            ),
            Icon(Icons.speed, size: 16, color: Fleet.ink500),
            const SizedBox(width: 8),
            SizedBox(
              width: 52,
              child: Text(
                _speedSet ? '${_speed.toStringAsFixed(2)}x' : 'default',
                textAlign: TextAlign.end,
                style: TextStyle(color: Fleet.ink300, fontSize: 12),
              ),
            ),
          ],
        ),
        if (_speedSet)
          Align(
            alignment: Alignment.centerRight,
            child: TextButton(
              onPressed: _busy
                  ? null
                  : () {
                      setState(() {
                        _speed = 1.0;
                        _speedSet = false;
                      });
                      _save(_selected ?? '', close: false, speedOverride: 0);
                    },
              child: const Text('Reset to default'),
            ),
          ),
      ],
    );
  }
}
