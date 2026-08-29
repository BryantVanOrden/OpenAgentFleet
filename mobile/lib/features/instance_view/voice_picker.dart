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
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _selected = widget.instance.voice;
  }

  Future<void> _save(String voiceId) async {
    setState(() {
      _selected = voiceId;
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(apiProvider).setInstanceVoice(widget.instance.id, voiceId);
      ref.invalidate(instancesProvider);
      if (mounted) Navigator.pop(context, true);
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
}
