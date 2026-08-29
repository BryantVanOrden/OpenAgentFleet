import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';

/// Everything that starts work without you: cron wakeups and inbound webhooks.
class SchedulesScreen extends ConsumerWidget {
  const SchedulesScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final crons = ref.watch(cronTriggersProvider);
    final hooks = ref.watch(webhookTriggersProvider);

    return Scaffold(
      appBar: AppBar(
        title: const Text('Schedules & wakeups'),
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh),
            onPressed: () {
              ref.invalidate(cronTriggersProvider);
              ref.invalidate(webhookTriggersProvider);
            },
          ),
        ],
      ),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          _Section(
            icon: Icons.alarm,
            title: 'Scheduled wakeups',
            subtitle: 'Agents starting work on their own, on a schedule.',
            child: crons.when(
              loading: () => const LinearProgressIndicator(minHeight: 2),
              error: (e, _) => _Problem('$e'),
              data: (list) => list.isEmpty
                  ? const _Empty('Nothing is scheduled.')
                  : Column(children: [for (final c in list) _CronTile(c)]),
            ),
          ),
          const SizedBox(height: 20),
          _Section(
            icon: Icons.webhook_outlined,
            title: 'Inbound hooks',
            subtitle: 'Work started by something calling in from outside.',
            child: hooks.when(
              loading: () => const LinearProgressIndicator(minHeight: 2),
              error: (e, _) => _Problem('$e'),
              data: (list) => list.isEmpty
                  ? const _Empty('No hooks configured.')
                  : Column(children: [for (final h in list) _HookTile(h)]),
            ),
          ),
        ],
      ),
    );
  }
}

class _CronTile extends StatelessWidget {
  const _CronTile(this.trigger);
  final CronTrigger trigger;

  @override
  Widget build(BuildContext context) {
    final last = trigger.lastRunAt;
    return Padding(
      padding: const EdgeInsets.only(top: 10),
      child: Container(
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: Fleet.ink950,
          borderRadius: BorderRadius.circular(8),
          border: Border.all(color: Fleet.ink800),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(trigger.name,
                      style: const TextStyle(
                          fontSize: 13, fontWeight: FontWeight.w600)),
                ),
                // Status reflects the record rather than being decorative:
                // a paused trigger looked "Active" in the old console.
                Container(
                  padding:
                      const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                  decoration: BoxDecoration(
                    color: (trigger.active ? Fleet.good : Fleet.ink600)
                        .withValues(alpha: 0.18),
                    borderRadius: BorderRadius.circular(5),
                  ),
                  child: Text(
                    trigger.active ? 'active' : 'paused',
                    style: TextStyle(
                      fontSize: 9,
                      color: trigger.active ? Fleet.good : Fleet.ink300,
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 6),
            Row(
              children: [
                Icon(Icons.schedule, size: 12, color: Fleet.ink400),
                const SizedBox(width: 5),
                Text(trigger.scheduleCron,
                    style: TextStyle(
                        fontFamily: 'monospace',
                        fontSize: 11,
                        color: Fleet.ink200)),
              ],
            ),
            if (trigger.goalTemplate.isNotEmpty) ...[
              const SizedBox(height: 6),
              Text(trigger.goalTemplate,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(color: Fleet.ink300, fontSize: 11)),
            ],
            const SizedBox(height: 6),
            Text(
              last == null ? 'Never run yet' : 'Last run ${humanAgo(last)}',
              style: TextStyle(color: Fleet.ink400, fontSize: 10),
            ),
          ],
        ),
      ),
    );
  }
}

class _HookTile extends StatelessWidget {
  const _HookTile(this.hook);
  final WebhookTrigger hook;

  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.only(top: 10),
        child: Container(
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: Fleet.ink950,
            borderRadius: BorderRadius.circular(8),
            border: Border.all(color: Fleet.ink800),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(hook.name,
                  style: const TextStyle(
                      fontSize: 13, fontWeight: FontWeight.w600)),
              if (hook.goalTemplate.isNotEmpty) ...[
                const SizedBox(height: 5),
                Text(hook.goalTemplate,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(color: Fleet.ink300, fontSize: 11)),
              ],
              const SizedBox(height: 5),
              Text(
                hook.targetInstanceId.isNotEmpty
                    ? 'Targets instance ${hook.targetInstanceId}'
                    : 'Targets archetype ${hook.targetArchetype}',
                style: TextStyle(color: Fleet.ink400, fontSize: 10),
              ),
            ],
          ),
        ),
      );
}

class _Section extends StatelessWidget {
  const _Section({
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.child,
  });

  final IconData icon;
  final String title;
  final String subtitle;
  final Widget child;

  @override
  Widget build(BuildContext context) => Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(icon, size: 18),
              const SizedBox(width: 8),
              Text(title, style: Theme.of(context).textTheme.titleSmall),
            ],
          ),
          const SizedBox(height: 3),
          Text(subtitle,
              style: TextStyle(color: Fleet.ink400, fontSize: 12)),
          child,
        ],
      );
}

class _Empty extends StatelessWidget {
  const _Empty(this.message);
  final String message;

  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.only(top: 12),
        child: Text(message,
            style: TextStyle(color: Fleet.ink400, fontSize: 12)),
      );
}

class _Problem extends StatelessWidget {
  const _Problem(this.message);
  final String message;

  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.only(top: 12),
        child: Text(message, style: TextStyle(color: Fleet.bad, fontSize: 12)),
      );
}
