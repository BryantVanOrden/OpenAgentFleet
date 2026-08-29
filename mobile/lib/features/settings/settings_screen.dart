import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/state.dart';
import '../../core/theme/theme.dart';
import 'host_usage_card.dart';
import '../auth/login_screen.dart';
import 'theme_card.dart';

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
          const ThemeCard(),
          const SizedBox(height: 12),
          Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text('Connection',
                      style: TextStyle(fontWeight: FontWeight.w600)),
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
                  const Text('Notifications',
                      style: TextStyle(fontWeight: FontWeight.w600)),
                  const SizedBox(height: 8),
                  Text(
                    'This device is registered for push when the app has a Firebase '
                    'configuration. Critical alerts — an agent stuck on a CAPTCHA or an MFA '
                    'prompt — are sent time-sensitive so they surface through Focus and Do Not '
                    'Disturb.',
                    style: TextStyle(
                        color: Fleet.ink400, fontSize: 12, height: 1.4),
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(height: 12),
          _AIConnectionsCard(),
          const SizedBox(height: 12),
          Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text('What this app can do',
                      style: TextStyle(fontWeight: FontWeight.w600)),
                  const SizedBox(height: 8),
                  Text(
                    'Watch instances, provision new agents, answer agents that are blocked, '
                    'take over a desktop, talk to an agent by voice, and start or stop runs.\n\n'
                    'Model configuration and access control still live in the web console — '
                    'those are administration rather than triage, and a phone is the wrong '
                    'place for them.',
                    style: TextStyle(
                        color: Fleet.ink400, fontSize: 12, height: 1.4),
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(height: 20),
          const HostUsageCard(),
          const SizedBox(height: 20),
          OutlinedButton.icon(
            style: OutlinedButton.styleFrom(
              foregroundColor: Fleet.bad,
              side: BorderSide(color: Fleet.bad),
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

class _AIConnectionsCard extends ConsumerWidget {
  const _AIConnectionsCard();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final providersAsync = ref.watch(aiProvidersProvider);
    final api = ref.watch(apiProvider);

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                const Text(
                  'AI Connections & Tiered Fallback',
                  style: TextStyle(fontWeight: FontWeight.w600),
                ),
                IconButton(
                  icon: const Icon(Icons.refresh, size: 18),
                  onPressed: () => ref.invalidate(aiProvidersProvider),
                  padding: EdgeInsets.zero,
                  constraints: const BoxConstraints(),
                  tooltip: 'Refresh AI engines',
                ),
              ],
            ),
            const SizedBox(height: 6),
            Text(
              'If your primary model runs out of credits, hits rate limits, or fails, the orchestrator fails over to the next tier automatically.',
              style: TextStyle(color: Fleet.ink400, fontSize: 12, height: 1.4),
            ),
            const SizedBox(height: 12),
            providersAsync.when(
              loading: () => const Center(
                child: Padding(
                  padding: EdgeInsets.all(12),
                  child: CircularProgressIndicator(strokeWidth: 2),
                ),
              ),
              error: (err, _) => Text(
                'Could not load AI engines: $err',
                style: TextStyle(color: Fleet.bad, fontSize: 12),
              ),
              data: (list) {
                if (list.isEmpty) {
                  return Text(
                    'No engines configured. Add providers in the Web Console.',
                    style: TextStyle(color: Fleet.ink400, fontSize: 12),
                  );
                }
                return Column(
                  children: List.generate(list.length, (idx) {
                    final p = list[idx];
                    final isFirst = idx == 0;
                    final isLast = idx == list.length - 1;

                    return Container(
                      margin: const EdgeInsets.only(bottom: 8),
                      padding: const EdgeInsets.all(10),
                      decoration: BoxDecoration(
                        color: Fleet.ink900,
                        borderRadius: BorderRadius.circular(8),
                        border: Border.all(
                          color: p.enabled ? Fleet.ink700 : Fleet.ink850,
                        ),
                      ),
                      child: Row(
                        children: [
                          Container(
                            padding: const EdgeInsets.symmetric(
                                horizontal: 6, vertical: 2),
                            decoration: BoxDecoration(
                              color: idx == 0
                                  ? Fleet.good.withValues(alpha: 0.2)
                                  : idx == 1
                                      ? Colors.cyan.withValues(alpha: 0.2)
                                      : idx == 2
                                          ? Colors.purple.withValues(alpha: 0.2)
                                          : Fleet.ink800,
                              borderRadius: BorderRadius.circular(4),
                            ),
                            child: Text(
                              'Tier ${idx + 1}',
                              style: TextStyle(
                                fontSize: 10,
                                fontWeight: FontWeight.bold,
                                color: idx == 0
                                    ? Fleet.good
                                    : idx == 1
                                        ? Colors.cyan
                                        : idx == 2
                                            ? Colors.purpleAccent
                                            : Fleet.ink400,
                              ),
                            ),
                          ),
                          const SizedBox(width: 10),
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Row(
                                  children: [
                                    Text(
                                      p.name,
                                      style: const TextStyle(
                                          fontWeight: FontWeight.w600,
                                          fontSize: 13),
                                    ),
                                    const SizedBox(width: 6),
                                    if (idx == 0)
                                      Text(
                                        '★ Primary',
                                        style: TextStyle(
                                            color: Fleet.good, fontSize: 11),
                                      ),
                                  ],
                                ),
                                Text(
                                  '${p.model} · ${p.kind}',
                                  style: TextStyle(
                                      color: Fleet.ink400,
                                      fontSize: 11,
                                      fontFamily: 'monospace'),
                                ),
                              ],
                            ),
                          ),
                          IconButton(
                            icon: const Icon(Icons.arrow_upward, size: 16),
                            onPressed: isFirst
                                ? null
                                : () async {
                                    final copy = [...list];
                                    final moved = copy.removeAt(idx);
                                    copy.insert(idx - 1, moved);
                                    await api.reorderProviders(
                                        copy.map((x) => x.id).toList());
                                    ref.invalidate(aiProvidersProvider);
                                  },
                            padding: EdgeInsets.zero,
                            constraints: const BoxConstraints(
                                minWidth: 28, minHeight: 28),
                            color: Fleet.ink300,
                            disabledColor: Fleet.ink700,
                          ),
                          IconButton(
                            icon: const Icon(Icons.arrow_downward, size: 16),
                            onPressed: isLast
                                ? null
                                : () async {
                                    final copy = [...list];
                                    final moved = copy.removeAt(idx);
                                    copy.insert(idx + 1, moved);
                                    await api.reorderProviders(
                                        copy.map((x) => x.id).toList());
                                    ref.invalidate(aiProvidersProvider);
                                  },
                            padding: EdgeInsets.zero,
                            constraints: const BoxConstraints(
                                minWidth: 28, minHeight: 28),
                            color: Fleet.ink300,
                            disabledColor: Fleet.ink700,
                          ),
                        ],
                      ),
                    );
                  }),
                );
              },
            ),
          ],
        ),
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
              child: Text(label,
                  style: TextStyle(color: Fleet.ink400, fontSize: 13)),
            ),
            Expanded(
              child: Text(
                value.isEmpty ? '—' : value,
                style:
                    TextStyle(color: valueColor ?? Fleet.ink100, fontSize: 13),
              ),
            ),
          ],
        ),
      );
}

