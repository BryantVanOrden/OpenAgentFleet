import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../instance_view/instance_screen.dart';

/// The resolution centre — the reason this app exists.
///
/// An agent that hits a CAPTCHA, an MFA prompt or a missing dependency stops and
/// asks. Whatever the operator types here is handed back as authoritative
/// instruction, ranked above anything the agent can see on its own screen.
class AlertsScreen extends ConsumerWidget {
  const AlertsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final alerts = ref.watch(alertsProvider);

    return Scaffold(
      appBar: AppBar(title: const Text('Alerts')),
      body: alerts.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (err, _) => Center(child: Text('$err', style: const TextStyle(color: Fleet.bad))),
        data: (list) {
          final blocking = list.where((a) => a.isOpen).toList();
          final history = list.where((a) => !a.isOpen).toList();

          if (list.isEmpty) {
            return const Center(
              child: Padding(
                padding: EdgeInsets.all(32),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(Icons.check_circle_outline, size: 44, color: Fleet.good),
                    SizedBox(height: 14),
                    Text('Nothing needs you'),
                    SizedBox(height: 4),
                    Text(
                      'Agents are working or idle.',
                      style: TextStyle(color: Fleet.ink400, fontSize: 13),
                    ),
                  ],
                ),
              ),
            );
          }

          return RefreshIndicator(
            onRefresh: () async => ref.invalidate(alertsProvider),
            child: ListView(
              padding: const EdgeInsets.fromLTRB(16, 8, 16, 24),
              children: [
                if (blocking.isNotEmpty) ...[
                  const _SectionLabel('Waiting on you', color: Fleet.warn),
                  for (final a in blocking) _AlertCard(alert: a),
                  const SizedBox(height: 20),
                ],
                if (history.isNotEmpty) ...[
                  const _SectionLabel('History'),
                  for (final a in history) _AlertCard(alert: a),
                ],
              ],
            ),
          );
        },
      ),
    );
  }
}

class _SectionLabel extends StatelessWidget {
  const _SectionLabel(this.text, {this.color = Fleet.ink400});
  final String text;
  final Color color;

  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.only(bottom: 10, top: 4),
        child: Text(
          text.toUpperCase(),
          style: TextStyle(
            color: color,
            fontSize: 11,
            fontWeight: FontWeight.w600,
            letterSpacing: 0.8,
          ),
        ),
      );
}

class _AlertCard extends ConsumerStatefulWidget {
  const _AlertCard({required this.alert});
  final Alert alert;

  @override
  ConsumerState<_AlertCard> createState() => _AlertCardState();
}

class _AlertCardState extends ConsumerState<_AlertCard> {
  final _reply = TextEditingController();
  bool _busy = false;

  @override
  void dispose() {
    _reply.dispose();
    super.dispose();
  }

  Future<void> _send(String text) async {
    setState(() => _busy = true);
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(apiProvider).replyAlert(widget.alert.id, text);
      ref.invalidate(alertsProvider);
      messenger.showSnackBar(const SnackBar(content: Text('Sent — the agent is resuming.')));
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final alert = widget.alert;
    final api = ref.read(apiProvider);
    final accent = Fleet.forState(alert.severity == 'critical' ? 'error' : alert.severity);

    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: Card(
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(14),
          side: BorderSide(color: alert.isOpen ? Fleet.warn.withValues(alpha: 0.4) : Fleet.ink700),
        ),
        child: Padding(
          padding: const EdgeInsets.all(14),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(_iconFor(alert.kind), size: 18, color: accent),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      alert.title,
                      style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600),
                    ),
                  ),
                  Text(
                    humanAgo(alert.createdAt),
                    style: const TextStyle(color: Fleet.ink400, fontSize: 11),
                  ),
                ],
              ),

              if (alert.screenshotId.isNotEmpty) ...[
                const SizedBox(height: 12),
                ClipRRect(
                  borderRadius: BorderRadius.circular(10),
                  child: Image.network(
                    api.artifactUrl(alert.screenshotId),
                    height: 160,
                    width: double.infinity,
                    fit: BoxFit.cover,
                    errorBuilder: (_, __, ___) => const SizedBox.shrink(),
                  ),
                ),
              ],

              const SizedBox(height: 10),
              Text(alert.body, style: const TextStyle(color: Fleet.ink300, fontSize: 13)),

              if (alert.reply.isNotEmpty) ...[
                const SizedBox(height: 10),
                Container(
                  width: double.infinity,
                  padding: const EdgeInsets.all(10),
                  decoration: BoxDecoration(
                    color: Fleet.ink850,
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: Text(
                    'You replied: ${alert.reply}',
                    style: const TextStyle(color: Fleet.ink300, fontSize: 12),
                  ),
                ),
              ],

              if (alert.instanceId.isNotEmpty) ...[
                const SizedBox(height: 6),
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton.icon(
                    onPressed: () => Navigator.of(context).push(
                      MaterialPageRoute(
                        builder: (_) => InstanceScreen(instanceId: alert.instanceId),
                      ),
                    ),
                    icon: const Icon(Icons.open_in_new, size: 16),
                    label: const Text('Open the machine'),
                  ),
                ),
              ],

              if (alert.isOpen) ...[
                const SizedBox(height: 6),
                TextField(
                  controller: _reply,
                  enabled: !_busy,
                  minLines: 2,
                  maxLines: 5,
                  decoration: const InputDecoration(
                    hintText: 'Tell the agent what to do. This outranks anything on its screen.',
                  ),
                ),
                const SizedBox(height: 10),
                Row(
                  children: [
                    Expanded(
                      child: FilledButton(
                        onPressed: _busy ? null : () => _send(_reply.text.trim()),
                        child: const Text('Send and resume'),
                      ),
                    ),
                    const SizedBox(width: 10),
                    OutlinedButton(
                      onPressed: _busy ? null : () => _send(''),
                      child: const Text('Acknowledge'),
                    ),
                  ],
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }

  IconData _iconFor(String kind) => switch (kind) {
        'needs_human' => Icons.pan_tool_outlined,
        'stalled' => Icons.hourglass_disabled_outlined,
        'failed' => Icons.error_outline,
        'completed' => Icons.check_circle_outline,
        'resource' => Icons.memory_outlined,
        _ => Icons.info_outline,
      };
}
