import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../auth/login_screen.dart';

class SettingsScreen extends ConsumerWidget {
  const SettingsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final api = ref.watch(apiProvider);
    final connected = ref.watch(connectionProvider).valueOrNull ?? false;

    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text('Connection', style: TextStyle(fontWeight: FontWeight.w600)),
                  const SizedBox(height: 10),
                  _Row(label: 'Orchestrator', value: api.baseUrl),
                  _Row(
                    label: 'Event stream',
                    value: connected ? 'connected' : 'reconnecting',
                    valueColor: connected ? Fleet.good : Fleet.warn,
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(height: 12),

          Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text('Notifications', style: TextStyle(fontWeight: FontWeight.w600)),
                  const SizedBox(height: 8),
                  const Text(
                    'This device is registered for push when the app has a Firebase '
                    'configuration. Critical alerts — an agent stuck on a CAPTCHA or an MFA '
                    'prompt — are sent time-sensitive so they surface through Focus and Do Not '
                    'Disturb.',
                    style: TextStyle(color: Fleet.ink400, fontSize: 12, height: 1.4),
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(height: 12),

          Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text('What this app can do', style: TextStyle(fontWeight: FontWeight.w600)),
                  const SizedBox(height: 8),
                  const Text(
                    'Watch instances, answer agents that are blocked, take over a desktop, and '
                    'start or stop runs.\n\n'
                    'Provisioning, recording skills, model configuration and access control live '
                    'in the web console — they are administration, not triage, and a phone is the '
                    'wrong place to do them.',
                    style: TextStyle(color: Fleet.ink400, fontSize: 12, height: 1.4),
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(height: 20),

          OutlinedButton.icon(
            style: OutlinedButton.styleFrom(
              foregroundColor: Fleet.bad,
              side: const BorderSide(color: Fleet.bad),
            ),
            onPressed: () async {
              await api.logout();
              ref.read(sessionProvider.notifier).state++;
              if (!context.mounted) return;
              Navigator.of(context).pushAndRemoveUntil(
                MaterialPageRoute(builder: (_) => const LoginScreen()),
                (_) => false,
              );
            },
            icon: const Icon(Icons.logout, size: 18),
            label: const Text('Sign out'),
          ),
        ],
      ),
    );
  }
}

class _Row extends StatelessWidget {
  const _Row({required this.label, required this.value, this.valueColor});

  final String label;
  final String value;
  final Color? valueColor;

  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.symmetric(vertical: 4),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SizedBox(
              width: 110,
              child: Text(label, style: const TextStyle(color: Fleet.ink400, fontSize: 13)),
            ),
            Expanded(
              child: Text(
                value.isEmpty ? '—' : value,
                style: TextStyle(color: valueColor ?? Fleet.ink100, fontSize: 13),
              ),
            ),
          ],
        ),
      );
}
