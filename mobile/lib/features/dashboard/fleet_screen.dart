import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../fleet_comms/comms_screen.dart';
import 'provision_sheet.dart';
import '../instance_view/instance_screen.dart';

/// The fleet at a glance: what is running, how hard it is working, and what it
/// is working on.
class FleetScreen extends ConsumerWidget {
  const FleetScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final instances = ref.watch(instancesProvider);
    final stats = ref.watch(statsProvider).valueOrNull ?? const {};
    final connected = ref.watch(connectionProvider).valueOrNull ?? false;

    return Scaffold(
      // Provisioning is the thing you most want when an alert reaches you away
      // from the desk, so it gets the primary action rather than living only
      // in the web console.
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () async {
          final created = await ProvisionSheet.show(context);
          if (created == true) ref.invalidate(instancesProvider);
        },
        icon: const Icon(Icons.add),
        label: const Text('New agent'),
      ),
      appBar: AppBar(
        title: const Text('Fleet'),
        actions: [
          // The agents' own conversation belongs beside the agents, not filed
          // under Vault with the credentials.
          IconButton(
            tooltip: 'Fleet comms',
            icon: const Icon(Icons.forum_outlined),
            onPressed: () => Navigator.of(context).push(
              MaterialPageRoute(builder: (_) => const CommsScreen()),
            ),
          ),
          Padding(
            padding: const EdgeInsets.only(right: 16),
            child: Row(
              children: [
                Container(
                  width: 6,
                  height: 6,
                  decoration: BoxDecoration(
                    color: connected ? Fleet.good : Fleet.bad,
                    shape: BoxShape.circle,
                  ),
                ),
                const SizedBox(width: 6),
                Text(
                  connected ? 'live' : 'offline',
                  style: TextStyle(color: Fleet.ink400, fontSize: 12),
                ),
              ],
            ),
          ),
        ],
      ),
      body: instances.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (err, _) => _ErrorState(
          message: '$err',
          onRetry: () => ref.invalidate(instancesProvider),
        ),
        data: (list) {
          if (list.isEmpty) {
            return const _EmptyState();
          }
          return RefreshIndicator(
            onRefresh: () async => ref.invalidate(instancesProvider),
            child: ListView.separated(
              padding: const EdgeInsets.fromLTRB(16, 8, 16, 24),
              itemCount: list.length,
              separatorBuilder: (_, __) => const SizedBox(height: 12),
              itemBuilder: (context, i) => _InstanceCard(
                instance: list[i],
                stats: stats[list[i].id],
              ),
            ),
          );
        },
      ),
    );
  }
}

class _InstanceCard extends ConsumerWidget {
  const _InstanceCard({required this.instance, this.stats});

  final Instance instance;
  final InstanceStats? stats;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final tasks =
        ref.watch(tasksProvider(instance.id)).valueOrNull ?? const <Task>[];
    Task? live;
    for (final t in tasks) {
      if (t.isLive) {
        live = t;
        break;
      }
    }

    final memLimit =
        stats?.memoryLimit ?? instance.profile.memoryMb * 1024 * 1024;
    final cpuMax = instance.profile.vcpu * 100;

    return Card(
      child: InkWell(
        borderRadius: BorderRadius.circular(14),
        onTap: () => Navigator.of(context).push(
          MaterialPageRoute(
              builder: (_) => InstanceScreen(instanceId: instance.id)),
        ),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          instance.name,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                              fontSize: 16, fontWeight: FontWeight.w600),
                        ),
                        const SizedBox(height: 2),
                        Text(
                          '${instance.tier} · ${instance.profile.vcpu.toStringAsFixed(0)} vCPU · '
                          '${(instance.profile.memoryMb / 1024).toStringAsFixed(0)} GB'
                          '${instance.profile.gpu ? " · GPU" : ""}',
                          style: TextStyle(color: Fleet.ink400, fontSize: 12),
                        ),
                      ],
                    ),
                  ),
                  StateChip(state: instance.state, live: instance.isRunning),
                ],
              ),
              if (instance.lastError.isNotEmpty) ...[
                const SizedBox(height: 10),
                Container(
                  width: double.infinity,
                  padding: const EdgeInsets.all(10),
                  decoration: BoxDecoration(
                    color: Fleet.bad.withValues(alpha: 0.1),
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: Text(
                    instance.lastError,
                    style: TextStyle(color: Fleet.bad, fontSize: 12),
                  ),
                ),
              ],
              const SizedBox(height: 14),
              _Meter(label: 'CPU', value: stats?.cpuPercent ?? 0, max: cpuMax),
              const SizedBox(height: 8),
              _Meter(
                label: 'Memory',
                value: (stats?.memoryBytes ?? 0).toDouble(),
                max: memLimit.toDouble(),
                trailing: humanBytes(stats?.memoryBytes ?? 0),
              ),
              if (live != null) ...[
                const SizedBox(height: 14),
                Container(
                  padding: const EdgeInsets.all(12),
                  decoration: BoxDecoration(
                    color: Fleet.ink850,
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: Row(
                    children: [
                      Icon(
                        live.state == 'awaiting_human'
                            ? Icons.pan_tool_outlined
                            : Icons.play_circle_outline,
                        size: 18,
                        color: Fleet.forState(live.state),
                      ),
                      const SizedBox(width: 10),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              live.goal,
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                              style: const TextStyle(fontSize: 13),
                            ),
                            Text(
                              'step ${live.step}/${live.maxSteps} · ${humanAgo(live.createdAt)}',
                              style:
                                  TextStyle(color: Fleet.ink400, fontSize: 11),
                            ),
                          ],
                        ),
                      ),
                    ],
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _Meter extends StatelessWidget {
  const _Meter(
      {required this.label,
      required this.value,
      required this.max,
      this.trailing});

  final String label;
  final double value;
  final double max;
  final String? trailing;

  @override
  Widget build(BuildContext context) {
    final fraction = max > 0 ? (value / max).clamp(0.0, 1.0) : 0.0;
    final color = fraction > 0.9
        ? Fleet.bad
        : fraction > 0.75
            ? Fleet.live
            : Fleet.good;

    return Column(
      children: [
        Row(
          children: [
            Text(label, style: TextStyle(color: Fleet.ink400, fontSize: 11)),
            const Spacer(),
            Text(
              trailing ?? '${(fraction * 100).toStringAsFixed(0)}%',
              style: TextStyle(
                color: Fleet.ink300,
                fontSize: 11,
                fontFeatures: [FontFeature.tabularFigures()],
              ),
            ),
          ],
        ),
        const SizedBox(height: 4),
        ClipRRect(
          borderRadius: BorderRadius.circular(999),
          child: LinearProgressIndicator(
            value: fraction,
            minHeight: 5,
            backgroundColor: Fleet.ink800,
            valueColor: AlwaysStoppedAnimation(color),
          ),
        ),
      ],
    );
  }
}

class _EmptyState extends StatelessWidget {
  const _EmptyState();

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.dns_outlined, size: 48, color: Fleet.ink600),
            SizedBox(height: 16),
            Text('No instances yet', style: TextStyle(fontSize: 16)),
            SizedBox(height: 6),
            Text(
              'Provision one from the web console. This app is for watching and '
              'unblocking them once they are running.',
              textAlign: TextAlign.center,
              style: TextStyle(color: Fleet.ink400, fontSize: 13),
            ),
          ],
        ),
      ),
    );
  }
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.cloud_off_outlined, size: 44, color: Fleet.bad),
            const SizedBox(height: 14),
            Text(
              message,
              textAlign: TextAlign.center,
              style: TextStyle(color: Fleet.ink300, fontSize: 13),
            ),
            const SizedBox(height: 18),
            OutlinedButton(onPressed: onRetry, child: const Text('Retry')),
          ],
        ),
      ),
    );
  }
}
