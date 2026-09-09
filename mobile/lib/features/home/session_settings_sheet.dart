import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';

/// Device, working folder and model for one session with Oaf. The folder has
/// to be one the device exposes; the server refuses anything else, and the
/// sheet says so in place rather than failing quietly.
class SessionSettingsSheet extends ConsumerStatefulWidget {
  const SessionSettingsSheet({super.key, required this.session, required this.devices, this.providers = const []});

  final OafSession session;
  final List<OafDevice> devices;
  final List<AIProvider> providers;

  @override
  ConsumerState<SessionSettingsSheet> createState() => _SessionSettingsSheetState();
}

class _SessionSettingsSheetState extends ConsumerState<SessionSettingsSheet> {
  late String _deviceId = widget.session.deviceId;
  late String _providerId = widget.session.providerId;
  late final _cwd = TextEditingController(text: widget.session.cwd);
  late final _name = TextEditingController(text: widget.session.name);
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _cwd.dispose();
    _name.dispose();
    super.dispose();
  }

  OafDevice? get _device => widget.devices.where((d) => d.id == _deviceId).firstOrNull;

  Future<void> _save() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final updated = await ref.read(apiProvider).updateOafSession(
            widget.session.id,
            name: _name.text.trim().isEmpty ? null : _name.text.trim(),
            deviceId: _deviceId,
            cwd: _cwd.text.trim(),
            providerId: _providerId,
          );
      if (mounted) Navigator.of(context).pop(updated);
    } catch (err) {
      setState(() {
        _error = '$err';
        _busy = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final device = _device;
    return Padding(
      padding: EdgeInsets.fromLTRB(16, 12, 16, 16 + MediaQuery.of(context).viewInsets.bottom),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text('Session', style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
          const SizedBox(height: 12),
          TextField(
            controller: _name,
            decoration: const InputDecoration(labelText: 'Name', isDense: true),
          ),
          const SizedBox(height: 12),
          DropdownButtonFormField<String>(
            initialValue: widget.devices.any((d) => d.id == _deviceId) ? _deviceId : '',
            decoration: const InputDecoration(labelText: 'Device', isDense: true),
            items: [
              const DropdownMenuItem(value: '', child: Text('None — fleet only')),
              for (final d in widget.devices)
                DropdownMenuItem(
                  value: d.id,
                  child: Text('${d.name} (${d.kind}${d.online ? '' : ', offline'})'),
                ),
            ],
            onChanged: (v) => setState(() => _deviceId = v ?? ''),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _cwd,
            enabled: _deviceId.isNotEmpty,
            style: const TextStyle(fontFamily: 'monospace', fontSize: 13),
            decoration: InputDecoration(
              labelText: 'Working folder',
              isDense: true,
              hintText: device?.roots.firstOrNull ?? 'C:\\Users\\you\\Code\\project',
              helperText: device == null
                  ? null
                  : device.roots.isEmpty
                      ? '${device.name} exposes no folders (a phone has none; a PC exposes what fleetctl host was given)'
                      : 'Under: ${device.roots.join(', ')}',
              helperMaxLines: 3,
            ),
          ),
          if (widget.providers.isNotEmpty) ...[
            const SizedBox(height: 12),
            DropdownButtonFormField<String>(
              initialValue: widget.providers.any((p) => p.id == _providerId) ? _providerId : '',
              decoration: const InputDecoration(labelText: 'Model', isDense: true),
              items: [
                const DropdownMenuItem(value: '', child: Text('Fleet default')),
                for (final p in widget.providers)
                  DropdownMenuItem(value: p.id, child: Text('${p.name} · ${p.model}', overflow: TextOverflow.ellipsis)),
              ],
              onChanged: (v) => setState(() => _providerId = v ?? ''),
            ),
          ],
          if (widget.devices.isEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 12),
              child: Text(
                'No devices yet. On your PC: pip install open-agent-fleet, then fleetctl host --root <folder>. '
                'For this phone: Settings → Let Oaf use this phone.',
                style: TextStyle(fontSize: 12, height: 1.4, color: Fleet.ink400),
              ),
            ),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.only(top: 10),
              child: Text(_error!, style: TextStyle(fontSize: 12, color: Fleet.bad)),
            ),
          const SizedBox(height: 16),
          Row(
            mainAxisAlignment: MainAxisAlignment.end,
            children: [
              TextButton(onPressed: _busy ? null : () => Navigator.of(context).pop(), child: const Text('Cancel')),
              const SizedBox(width: 8),
              FilledButton(onPressed: _busy ? null : _save, child: Text(_busy ? 'Saving…' : 'Save')),
            ],
          ),
        ],
      ),
    );
  }
}
