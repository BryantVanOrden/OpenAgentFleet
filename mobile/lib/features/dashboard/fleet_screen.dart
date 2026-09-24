import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/agent_kind_badge.dart';
import '../fleet_comms/comms_screen.dart';
import '../org/org_chart_screen.dart';
import '../pipelines/pipelines_screen.dart';
import '../skills/skills_screen.dart';
import '../work/work_screen.dart';
import 'add_agent_sheet.dart';
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
        // A kind first: a desktop goes on to the provisioning sheet, anything
        // else (Claude Code, Codex, OpenClaw, a webhook...) is a connection.
        onPressed: () async {
          final created = await AddAgentSheet.show(context);
          if (created == true) {
            ref.invalidate(instancesProvider);
            ref.invalidate(orgProvider);
          }
        },
        icon: const Icon(Icons.add),
        label: const Text('New agent'),
      ),
      appBar: AppBar(
        title: const Text('Fleet'),
        actions: [
          // Work and the org chart are about the agents on this screen --
          // what they are doing and who answers to whom -- so they sit here
          // with the rest of the fleet's surfaces rather than taking a tab.
          IconButton(
            tooltip: 'Work',
            icon: const Icon(Icons.view_kanban_outlined),
            onPressed: () => Navigator.of(context).push(
              MaterialPageRoute(builder: (_) => const WorkScreen()),
            ),
          ),
          IconButton(
            tooltip: 'Org chart',
            icon: const Icon(Icons.lan_outlined),
            onPressed: () => Navigator.of(context).push(
              MaterialPageRoute(builder: (_) => const OrgChartScreen()),
            ),
          ),
          // The agents' own conversation belongs beside the agents, not filed
          // under Vault with the credentials.
          IconButton(
            tooltip: 'Fleet comms',
            icon: const Icon(Icons.forum_outlined),
            onPressed: () => Navigator.of(context).push(
              MaterialPageRoute(builder: (_) => const CommsScreen()),
            ),
          ),
          // Five icons and the live dot do not fit a phone's bar, so on a
          // narrow screen Skills and Pipelines fold into a menu. On a tablet
          // they stay one tap away.
          if (MediaQuery.sizeOf(context).width >= 600) ...[
            // Skills are how operators teach the fleet, so they live beside
            // the agents rather than under Admin — using one changes nothing
            // about who may do what.
            IconButton(
              tooltip: 'Skills',
              icon: const Icon(Icons.psychology_outlined),
              onPressed: () => Navigator.of(context).push(
                MaterialPageRoute(builder: (_) => const SkillsScreen()),
              ),
            ),
            // Pipelines are the shape of work the fleet does without you.
            IconButton(
              tooltip: 'Pipelines',
              icon: const Icon(Icons.account_tree_outlined),
              onPressed: () => Navigator.of(context).push(
                MaterialPageRoute(builder: (_) => const PipelinesScreen()),
              ),
            ),
          ] else
            PopupMenuButton<String>(
              tooltip: 'More',
              color: Fleet.ink850,
              onSelected: (v) => Navigator.of(context).push(
                MaterialPageRoute(
                  builder: (_) => v == 'skills'
                      ? const SkillsScreen()
                      : const PipelinesScreen(),
                ),
              ),
              itemBuilder: (_) => const [
                PopupMenuItem(
                  value: 'skills',
                  child: ListTile(
                    dense: true,
                    leading: Icon(Icons.psychology_outlined),
                    title: Text('Skills'),
                  ),
                ),
                PopupMenuItem(
                  value: 'pipelines',
                  child: ListTile(
                    dense: true,
                    leading: Icon(Icons.account_tree_outlined),
                    title: Text('Pipelines'),
                  ),
                ),
              ],
            ),
          Padding(
            padding: const EdgeInsets.only(right: 12, left: 4),
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
    if (instance.isExternal) return _ExternalCard(instance: instance);
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

/// An agent that runs somewhere else. It has no sandbox, so no CPU or memory
/// to meter and no desktop to show: what it is, where it lives, and what it
/// is on.
class _ExternalCard extends ConsumerWidget {
  const _ExternalCard({required this.instance});

  final Instance instance;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final node = ref
        .watch(orgProvider)
        .valueOrNull
        ?.nodes
        .where((n) => n.id == instance.id)
        .firstOrNull;
    final devices = AgentKind.onDevice(instance.kind)
        ? ref.watch(oafDevicesProvider).valueOrNull ?? const <OafDevice>[]
        : const <OafDevice>[];
    final device = devices
        .where((d) => d.id == instance.connection.deviceId)
        .firstOrNull;
    final online = node?.online ?? device?.online;
    final held = instance.isHeld || (node?.hold.isNotEmpty ?? false);
    final busy = node?.busy ?? false;

    final String status;
    final Color dot;
    if (held) {
      status = 'Held at its budget';
      dot = Fleet.warn;
    } else if (online == false) {
      status = AgentKind.onDevice(instance.kind)
          ? 'Offline: its PC is not connected'
          : 'Unreachable';
      dot = Fleet.ink500;
    } else if (busy) {
      final title = node?.ticketTitle ?? '';
      status = 'Working on ${node?.ticketRef ?? 'a ticket'}'
          '${title.isEmpty ? '' : ' · $title'}';
      dot = Fleet.good;
    } else {
      status = 'Idle';
      dot = Fleet.ink300;
    }

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
                          [
                            if (instance.title.isNotEmpty) instance.title,
                            instance.connection.summary(
                                deviceName: device?.name ?? '', short: true),
                          ].join(' · '),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(color: Fleet.ink400, fontSize: 12),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(width: 8),
                  AgentKindBadge(instance.kind),
                ],
              ),
              const SizedBox(height: 12),
              Row(
                children: [
                  Container(
                    width: 7,
                    height: 7,
                    decoration:
                        BoxDecoration(color: dot, shape: BoxShape.circle),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      status,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(color: Fleet.ink300, fontSize: 12.5),
                    ),
                  ),
                  if ((node?.openTickets ?? 0) > 0)
                    Text('${node?.openTickets} open',
                        style: TextStyle(color: Fleet.ink400, fontSize: 11.5)),
                ],
              ),
              if (instance.lastError.isNotEmpty) ...[
                const SizedBox(height: 10),
                Text(instance.lastError,
                    maxLines: 3,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(color: Fleet.bad, fontSize: 12)),
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
            Text('Oaf is all alone here', style: TextStyle(fontSize: 16)),
            SizedBox(height: 6),
            Text(
              'No agents yet. Tap New agent to give Oaf some company.',
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
