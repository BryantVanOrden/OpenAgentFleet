import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';

/// Everything that starts work without you: cron wakeups and inbound webhooks.
/// Both kinds are authored here too — created on the phone, deleted with a
/// confirmation, because a trigger is a standing instruction and the person
/// reading an alert at 03:00 is exactly who needs to be able to stop one.
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
            onAdd: () => _CronSheet.show(context, ref),
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
            onAdd: () => _WebhookSheet.show(context, ref),
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

/// Confirm-then-delete, shared by both trigger kinds.
Future<void> _confirmDelete({
  required BuildContext context,
  required WidgetRef ref,
  required String name,
  required String consequence,
  required Future<void> Function() doDelete,
  required ProviderOrFamily invalidate,
}) async {
  final ok = await showDialog<bool>(
    context: context,
    builder: (ctx) => AlertDialog(
      title: Text('Delete "$name"?'),
      content: Text(consequence, style: TextStyle(color: Fleet.ink300)),
      actions: [
        TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel')),
        FilledButton(
          style: FilledButton.styleFrom(backgroundColor: Fleet.bad),
          onPressed: () => Navigator.pop(ctx, true),
          child: const Text('Delete'),
        ),
      ],
    ),
  );
  if (ok != true) return;
  try {
    await doDelete();
    ref.invalidate(invalidate);
  } catch (err) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('$err'), backgroundColor: Fleet.bad),
      );
    }
  }
}

class _CronTile extends ConsumerWidget {
  const _CronTile(this.trigger);
  final CronTrigger trigger;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
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
                _Pill(
                  trigger.active ? 'active' : 'paused',
                  color: trigger.active ? Fleet.good : Fleet.ink600,
                  textColor: trigger.active ? Fleet.good : Fleet.ink300,
                ),
                _DeleteButton(
                  onPressed: () => _confirmDelete(
                    context: context,
                    ref: ref,
                    name: trigger.name,
                    consequence:
                        'The schedule stops firing. Work already started by it '
                        'keeps running.',
                    doDelete: () =>
                        ref.read(apiProvider).deleteCronTrigger(trigger.id),
                    invalidate: cronTriggersProvider,
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
                const SizedBox(width: 10),
                if (trigger.targetArchetype.isNotEmpty)
                  _Pill('target: ${trigger.targetArchetype}',
                      color: Fleet.cool, textColor: Fleet.cool),
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

class _HookTile extends ConsumerWidget {
  const _HookTile(this.hook);
  final WebhookTrigger hook;

  @override
  Widget build(BuildContext context, WidgetRef ref) => Padding(
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
                    child: Text(hook.name,
                        style: const TextStyle(
                            fontSize: 13, fontWeight: FontWeight.w600)),
                  ),
                  _Pill(
                    hook.active ? 'active' : 'inactive',
                    color: hook.active ? Fleet.good : Fleet.ink600,
                    textColor: hook.active ? Fleet.good : Fleet.ink300,
                  ),
                  _DeleteButton(
                    onPressed: () => _confirmDelete(
                      context: context,
                      ref: ref,
                      name: hook.name,
                      consequence:
                          'The endpoint stops accepting deliveries immediately. '
                          'The sender keeps calling a URL that now refuses it.',
                      doDelete: () =>
                          ref.read(apiProvider).deleteWebhook(hook.id),
                      invalidate: webhookTriggersProvider,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 4),
              Wrap(
                spacing: 6,
                runSpacing: 4,
                children: [
                  // Which signature scheme applies. A Stripe webhook filed as
                  // generic rejects every delivery, so it has to be visible.
                  _Pill(hook.kind, color: Fleet.ink600, textColor: Fleet.ink300),
                  if (hook.targetArchetype.isNotEmpty)
                    _Pill('target: ${hook.targetArchetype}',
                        color: Fleet.cool, textColor: Fleet.cool),
                  if (!hook.hasSecret)
                    _Pill('no secret — will refuse deliveries',
                        color: Fleet.bad, textColor: Fleet.bad),
                ],
              ),
              if (hook.token.isNotEmpty) ...[
                const SizedBox(height: 8),
                Container(
                  width: double.infinity,
                  padding: const EdgeInsets.all(8),
                  decoration: BoxDecoration(
                    color: Fleet.ink900,
                    borderRadius: BorderRadius.circular(6),
                    border: Border.all(color: Fleet.ink800),
                  ),
                  child: SelectableText(
                    'POST /api/webhooks/${hook.token}',
                    style: TextStyle(
                        fontFamily: 'monospace',
                        fontSize: 10.5,
                        color: Fleet.live),
                  ),
                ),
              ],
              if (hook.goalTemplate.isNotEmpty) ...[
                const SizedBox(height: 6),
                Text(hook.goalTemplate,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(color: Fleet.ink300, fontSize: 11)),
              ],
              const SizedBox(height: 5),
              Text(
                hook.targetInstanceId.isNotEmpty
                    ? 'Targets instance ${hook.targetInstanceId}'
                    : hook.lastTriggeredAt != null
                        ? 'Last delivery ${humanAgo(hook.lastTriggeredAt!)}'
                        : 'No deliveries yet',
                style: TextStyle(color: Fleet.ink400, fontSize: 10),
              ),
            ],
          ),
        ),
      );
}

/// Create an inbound webhook.
class _WebhookSheet extends ConsumerStatefulWidget {
  const _WebhookSheet();

  static Future<void> show(BuildContext context, WidgetRef ref) async {
    final created = await showModalBottomSheet<bool>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Fleet.ink900,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
      ),
      builder: (_) => const _WebhookSheet(),
    );
    if (created == true) ref.invalidate(webhookTriggersProvider);
  }

  @override
  ConsumerState<_WebhookSheet> createState() => _WebhookSheetState();
}

class _WebhookSheetState extends ConsumerState<_WebhookSheet> {
  final _name = TextEditingController();
  final _token = TextEditingController();
  final _secret = TextEditingController();
  final _goal = TextEditingController();
  String _kind = 'generic';
  String? _archetype;
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _token.dispose();
    _secret.dispose();
    _goal.dispose();
    super.dispose();
  }

  /// A signing secret the operator can paste into the sending system.
  ///
  /// Random.secure rather than Random(): this is the only thing standing
  /// between an anonymous caller and an autonomous agent on the fleet.
  void _generateSecret() {
    final rng = Random.secure();
    final bytes = List<int>.generate(32, (_) => rng.nextInt(256));
    setState(() {
      _secret.text =
          bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
    });
  }

  bool get _complete =>
      _name.text.trim().isNotEmpty &&
      _goal.text.trim().isNotEmpty &&
      _secret.text.trim().isNotEmpty &&
      _archetype != null;

  Future<void> _create() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(apiProvider).createWebhook(
            name: _name.text.trim(),
            token: _token.text.trim(),
            kind: _kind,
            secret: _secret.text.trim(),
            targetArchetype: _archetype!,
            goalTemplate: _goal.text.trim(),
          );
      if (mounted) Navigator.pop(context, true);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final templates =
        ref.watch(templatesProvider).valueOrNull ?? const <BotTemplate>[];
    final inset = MediaQuery.of(context).viewInsets.bottom;

    return Padding(
      padding: EdgeInsets.fromLTRB(20, 18, 20, 18 + inset),
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                const Icon(Icons.webhook_outlined, size: 20),
                const SizedBox(width: 8),
                Text('New inbound webhook',
                    style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 16),
            TextField(
              controller: _name,
              autofocus: true,
              decoration: const InputDecoration(
                labelText: 'Name',
                hintText: 'e.g. GitHub pull request trigger',
              ),
              onChanged: (_) => setState(() {}),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _token,
              decoration: const InputDecoration(
                labelText: 'Custom ingress token (optional)',
                hintText: 'at least 24 characters, or blank',
                helperText: 'Blank has one generated for you.',
              ),
            ),
            const SizedBox(height: 12),
            DropdownButtonFormField<String>(
              initialValue: _kind,
              isExpanded: true,
              decoration: const InputDecoration(labelText: 'Sender'),
              dropdownColor: Fleet.ink850,
              items: [
                for (final k in WebhookTrigger.kinds)
                  DropdownMenuItem(value: k, child: Text(k)),
              ],
              onChanged: (v) => setState(() => _kind = v ?? 'generic'),
            ),
            const SizedBox(height: 4),
            Text(WebhookTrigger.kindHints[_kind] ?? '',
                style:
                    TextStyle(color: Fleet.ink400, fontSize: 11, height: 1.35)),
            const SizedBox(height: 12),
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: TextField(
                    controller: _secret,
                    style: const TextStyle(
                        fontFamily: 'monospace', fontSize: 12),
                    decoration: const InputDecoration(
                      labelText: 'Signing secret (required)',
                      hintText: "paste the sender's secret, or generate one",
                    ),
                    onChanged: (_) => setState(() {}),
                  ),
                ),
                const SizedBox(width: 8),
                OutlinedButton(
                  onPressed: _generateSecret,
                  child: const Text('Generate'),
                ),
              ],
            ),
            const SizedBox(height: 4),
            Text(
              'This endpoint takes no other authentication and starts real '
              'work, so an unsigned webhook is refused. It is never shown '
              'again after this.',
              style:
                  TextStyle(color: Fleet.ink400, fontSize: 11, height: 1.35),
            ),
            const SizedBox(height: 12),
            _ArchetypeDropdown(
              value: _archetype,
              templates: templates,
              onChanged: (v) => setState(() => _archetype = v),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _goal,
              minLines: 2,
              maxLines: 4,
              decoration: const InputDecoration(
                labelText: 'Goal template',
                hintText: 'e.g. Pull the latest PR code, run the test suite, '
                    'and comment feedback. {{payload}} interpolates the body.',
              ),
              onChanged: (_) => setState(() {}),
            ),
            InlineError(_error),
            const SizedBox(height: 14),
            FilledButton(
              onPressed: _busy || !_complete ? null : _create,
              child: Text(_busy ? 'Creating...' : 'Create webhook'),
            ),
          ],
        ),
      ),
    );
  }
}

/// Create a scheduled cron wakeup.
class _CronSheet extends ConsumerStatefulWidget {
  const _CronSheet();

  static Future<void> show(BuildContext context, WidgetRef ref) async {
    final created = await showModalBottomSheet<bool>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Fleet.ink900,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
      ),
      builder: (_) => const _CronSheet(),
    );
    if (created == true) ref.invalidate(cronTriggersProvider);
  }

  @override
  ConsumerState<_CronSheet> createState() => _CronSheetState();
}

class _CronSheetState extends ConsumerState<_CronSheet> {
  final _name = TextEditingController();
  final _cron = TextEditingController(text: '0 * * * *');
  final _goal = TextEditingController();
  String? _archetype;
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _cron.dispose();
    _goal.dispose();
    super.dispose();
  }

  bool get _complete =>
      _name.text.trim().isNotEmpty &&
      _cron.text.trim().isNotEmpty &&
      _goal.text.trim().isNotEmpty &&
      _archetype != null;

  Future<void> _create() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(apiProvider).createCronTrigger(
            name: _name.text.trim(),
            scheduleCron: _cron.text.trim(),
            targetArchetype: _archetype!,
            goalTemplate: _goal.text.trim(),
          );
      if (mounted) Navigator.pop(context, true);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final templates =
        ref.watch(templatesProvider).valueOrNull ?? const <BotTemplate>[];
    final inset = MediaQuery.of(context).viewInsets.bottom;

    return Padding(
      padding: EdgeInsets.fromLTRB(20, 18, 20, 18 + inset),
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                const Icon(Icons.alarm_add_outlined, size: 20),
                const SizedBox(width: 8),
                Text('New scheduled wakeup',
                    style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 16),
            TextField(
              controller: _name,
              autofocus: true,
              decoration: const InputDecoration(
                labelText: 'Name',
                hintText: 'e.g. Nightly security sweep',
              ),
              onChanged: (_) => setState(() {}),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _cron,
              style: const TextStyle(fontFamily: 'monospace', fontSize: 13),
              decoration: const InputDecoration(
                labelText: 'Cron expression',
                helperText: 'Standard five-field cron, e.g. 0 2 * * *',
              ),
              onChanged: (_) => setState(() {}),
            ),
            const SizedBox(height: 12),
            _ArchetypeDropdown(
              value: _archetype,
              templates: templates,
              onChanged: (v) => setState(() => _archetype = v),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _goal,
              minLines: 2,
              maxLines: 4,
              decoration: const InputDecoration(
                labelText: 'Goal template',
                hintText:
                    'e.g. Run a SAST audit on the repository and export findings.',
              ),
              onChanged: (_) => setState(() {}),
            ),
            InlineError(_error),
            const SizedBox(height: 14),
            FilledButton(
              onPressed: _busy || !_complete ? null : _create,
              child: Text(_busy ? 'Scheduling...' : 'Schedule trigger'),
            ),
          ],
        ),
      ),
    );
  }
}

/// Target archetype, from the server's own catalogue. Fetched rather than
/// hardcoded — the console ships a fixed list of five and cannot target any
/// archetype added since, which is a defect, not a behaviour to copy.
class _ArchetypeDropdown extends StatelessWidget {
  const _ArchetypeDropdown({
    required this.value,
    required this.templates,
    required this.onChanged,
  });

  final String? value;
  final List<BotTemplate> templates;
  final ValueChanged<String?> onChanged;

  @override
  Widget build(BuildContext context) => DropdownButtonFormField<String>(
        initialValue: value,
        isExpanded: true,
        decoration: const InputDecoration(labelText: 'Target archetype'),
        dropdownColor: Fleet.ink850,
        hint: templates.isEmpty
            ? const Text('Loading archetypes...')
            : const Text('Which kind of bot handles it'),
        items: [
          for (final t in templates)
            DropdownMenuItem(
              value: t.id,
              child: Text(t.name, overflow: TextOverflow.ellipsis),
            ),
        ],
        onChanged: onChanged,
      );
}

class _DeleteButton extends StatelessWidget {
  const _DeleteButton({required this.onPressed});
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) => IconButton(
        tooltip: 'Delete',
        icon: Icon(Icons.delete_outline, size: 18, color: Fleet.bad),
        padding: EdgeInsets.zero,
        constraints: const BoxConstraints(minWidth: 32, minHeight: 32),
        onPressed: onPressed,
      );
}

class _Pill extends StatelessWidget {
  const _Pill(this.label, {required this.color, required this.textColor});

  final String label;
  final Color color;
  final Color textColor;

  @override
  Widget build(BuildContext context) => Container(
        padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
        decoration: BoxDecoration(
          color: color.withValues(alpha: 0.18),
          borderRadius: BorderRadius.circular(5),
        ),
        child: Text(
          label,
          style: TextStyle(fontSize: 9, color: textColor),
        ),
      );
}

class _Section extends StatelessWidget {
  const _Section({
    required this.icon,
    required this.title,
    required this.subtitle,
    required this.child,
    this.onAdd,
  });

  final IconData icon;
  final String title;
  final String subtitle;
  final Widget child;
  final VoidCallback? onAdd;

  @override
  Widget build(BuildContext context) => Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(icon, size: 18),
              const SizedBox(width: 8),
              Expanded(
                child:
                    Text(title, style: Theme.of(context).textTheme.titleSmall),
              ),
              if (onAdd != null)
                TextButton.icon(
                  onPressed: onAdd,
                  icon: const Icon(Icons.add, size: 16),
                  label: const Text('Add', style: TextStyle(fontSize: 12)),
                ),
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
