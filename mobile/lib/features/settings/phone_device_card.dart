import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/device/phone_device_service.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/voice/voice_service.dart';

/// The phone as a device for Oaf, lives for the app's lifetime.
final phoneDeviceProvider = ChangeNotifierProvider<PhoneDeviceService>((ref) {
  final api = ref.read(apiProvider);
  final svc = PhoneDeviceService(api, VoiceService(api: api));
  svc.load();
  return svc;
});

/// Settings → "Let Oaf use this phone".
///
/// Switched on, the phone registers itself and, while the app is open, runs
/// the jobs a phone can: notify, open a link, read or set the clipboard, say
/// something. Any session can then pick it as its device.
class PhoneDeviceCard extends ConsumerWidget {
  const PhoneDeviceCard({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final svc = ref.watch(phoneDeviceProvider);
    final on = svc.enabled;
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 12, 12, 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.phone_iphone_rounded, size: 20, color: on ? Fleet.live : Fleet.ink400),
                const SizedBox(width: 10),
                const Expanded(
                  child: Text('Let Oaf use this phone', style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600)),
                ),
                Switch(
                  value: on,
                  onChanged: (v) => svc.setEnabled(v),
                ),
              ],
            ),
            const SizedBox(height: 6),
            Text(
              on
                  ? 'Registered as “${svc.name}” — ${svc.status}. While the app is open Oaf can send a notification here, '
                      'open a link, use the clipboard and speak. ${svc.handled} job${svc.handled == 1 ? '' : 's'} handled.'
                  : 'Registers this phone as a device a session can act on: notifications, links, clipboard, speech. '
                      'Files and commands need a PC running fleetctl host.',
              style: TextStyle(fontSize: 12, height: 1.4, color: Fleet.ink400),
            ),
            if (on)
              Align(
                alignment: Alignment.centerRight,
                child: TextButton.icon(
                  onPressed: () => _rename(context, svc),
                  icon: const Icon(Icons.edit_outlined, size: 16),
                  label: const Text('Rename'),
                ),
              ),
          ],
        ),
      ),
    );
  }

  Future<void> _rename(BuildContext context, PhoneDeviceService svc) async {
    final ctl = TextEditingController(text: svc.name);
    final name = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Device name'),
        content: TextField(controller: ctl, autofocus: true, onSubmitted: (v) => Navigator.pop(ctx, v)),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('Cancel')),
          FilledButton(onPressed: () => Navigator.pop(ctx, ctl.text), child: const Text('Save')),
        ],
      ),
    );
    if (name != null && name.trim().isNotEmpty) {
      await svc.stop();
      await svc.setEnabled(true, name: name.trim());
    }
  }
}
