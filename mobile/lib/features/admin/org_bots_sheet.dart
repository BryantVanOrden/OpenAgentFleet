import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';

/// Which bots belong to a department.
///
/// A bot can be in several at once, so this is a set of ticks rather than a
/// move: adding one here does not take it away from anywhere else. That is
/// the point — a triage bot support and engineering both rely on used to have
/// to be filed under one of them and be invisible to the other.
class OrgBotsSheet extends ConsumerStatefulWidget {
  const OrgBotsSheet({super.key, required this.org});

  final Org org;

  static Future<bool?> show(BuildContext context, Org org) =>
      showModalBottomSheet<bool>(
        context: context,
        isScrollControlled: true,
        backgroundColor: Fleet.ink900,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
        ),
        builder: (_) => OrgBotsSheet(org: org),
      );

  @override
  ConsumerState<OrgBotsSheet> createState() => _OrgBotsSheetState();
}

class _OrgBotsSheetState extends ConsumerState<OrgBotsSheet> {
  final Set<String> _busy = {};
  String? _error;

  Future<void> _toggle(Instance bot, bool inOrg) async {
    setState(() {
      _busy.add(bot.id);
      _error = null;
    });
    try {
      // Send the whole set: the server replaces it wholesale, so composing it
      // here keeps "what I am looking at" and "what I am sending" the same.
      final next = [...bot.orgIds];
      if (inOrg) {
        if (!next.contains(widget.org.id)) next.add(widget.org.id);
      } else {
        next.remove(widget.org.id);
      }
      await ref.read(apiProvider).setInstanceOrgs(bot.id, next);
      ref.invalidate(instancesProvider);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _busy.remove(bot.id));
    }
  }

  @override
  Widget build(BuildContext context) {
    final instances = ref.watch(instancesProvider);

    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 18, 20, 18),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                const Icon(Icons.smart_toy_outlined, size: 20),
                const SizedBox(width: 8),
                Expanded(
                  child: Text('Bots in ${widget.org.name}',
                      style: Theme.of(context).textTheme.titleMedium),
                ),
              ],
            ),
            const SizedBox(height: 6),
            Text(
              'A bot can belong to more than one department. Adding it here '
              'does not remove it from anywhere else.',
              style: TextStyle(color: Fleet.ink400, fontSize: 12, height: 1.4),
            ),
            if (_error != null) ...[
              const SizedBox(height: 10),
              InlineError(_error!),
            ],
            const SizedBox(height: 8),
            Flexible(
              child: instances.when(
                loading: () => const Padding(
                  padding: EdgeInsets.all(24),
                  child: Center(child: CircularProgressIndicator()),
                ),
                error: (e, _) => InlineError('Could not load bots: $e'),
                data: (list) {
                  if (list.isEmpty) {
                    return Padding(
                      padding: const EdgeInsets.all(24),
                      child: Text('No bots yet.',
                          textAlign: TextAlign.center,
                          style: TextStyle(color: Fleet.ink400)),
                    );
                  }
                  return ListView(
                    shrinkWrap: true,
                    children: [
                      for (final bot in list) _row(bot),
                    ],
                  );
                },
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _row(Instance bot) {
    final inOrg = bot.orgIds.contains(widget.org.id);
    final elsewhere = bot.orgIds.where((o) => o != widget.org.id).length;
    final working = _busy.contains(bot.id);

    return CheckboxListTile(
      dense: true,
      contentPadding: EdgeInsets.zero,
      value: inOrg,
      onChanged: working ? null : (v) => _toggle(bot, v ?? false),
      title: Text(bot.name, style: const TextStyle(fontSize: 14)),
      subtitle: Text(
        [
          if (elsewhere > 0)
            'also in $elsewhere other department${elsewhere == 1 ? '' : 's'}',
          if (bot.orgIds.isEmpty) 'not in any department',
        ].join(' · '),
        style: TextStyle(color: Fleet.ink400, fontSize: 11),
      ),
      secondary: working
          ? const SizedBox(
              width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2))
          : null,
    );
  }
}
