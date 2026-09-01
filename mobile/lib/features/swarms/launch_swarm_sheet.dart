import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';

/// Launch a swarm: a mission, and a team picked from bots that actually exist.
/// Each member gets the mission, its own role, and its teammates' names, then
/// starts a real task straight away.
class LaunchSwarmSheet extends ConsumerStatefulWidget {
  const LaunchSwarmSheet({super.key});

  /// Returns the created swarm, or null when dismissed.
  static Future<SwarmTeam?> show(BuildContext context) =>
      showModalBottomSheet<SwarmTeam>(
        context: context,
        isScrollControlled: true,
        backgroundColor: Fleet.ink900,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
        ),
        builder: (ctx) => Padding(
          padding:
              EdgeInsets.only(bottom: MediaQuery.of(ctx).viewInsets.bottom),
          child: const LaunchSwarmSheet(),
        ),
      );

  @override
  ConsumerState<LaunchSwarmSheet> createState() => _LaunchSwarmSheetState();
}

class _LaunchSwarmSheetState extends ConsumerState<LaunchSwarmSheet> {
  final _name = TextEditingController();
  final _goal = TextEditingController();

  /// instance id -> role on this mission. Presence in the map is the
  /// selection, so a picked bot always has a role and an unpicked one cannot
  /// carry a stale one from an earlier draft.
  final _roles = <String, String>{};

  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    // The launch button enables as the fields fill in.
    _name.addListener(_changed);
    _goal.addListener(_changed);
  }

  void _changed() => setState(() {});

  @override
  void dispose() {
    _name.dispose();
    _goal.dispose();
    super.dispose();
  }

  bool get _ready =>
      _name.text.trim().isNotEmpty &&
      _goal.text.trim().isNotEmpty &&
      _roles.isNotEmpty;

  Future<void> _launch() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final swarm = await ref.read(apiProvider).createSwarm(
        _name.text.trim(),
        _goal.text.trim(),
        members: [
          for (final e in _roles.entries)
            (
              instanceId: e.key,
              role: e.value.trim().isEmpty ? 'Contributor' : e.value.trim(),
            ),
        ],
      );
      if (mounted) Navigator.pop(context, swarm);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final instances =
        ref.watch(instancesProvider).valueOrNull ?? const <Instance>[];

    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 18, 20, 18),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                const Icon(Icons.rocket_launch_outlined, size: 20),
                const SizedBox(width: 8),
                Expanded(
                  child: Text('Launch a swarm',
                      style: Theme.of(context).textTheme.titleMedium),
                ),
              ],
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _name,
              enabled: !_busy,
              textCapitalization: TextCapitalization.sentences,
              decoration: const InputDecoration(
                labelText: 'Mission name',
                hintText: 'e.g. Release QA sweep',
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _goal,
              enabled: !_busy,
              minLines: 3,
              maxLines: 5,
              textCapitalization: TextCapitalization.sentences,
              decoration: const InputDecoration(
                labelText: 'Shared goal',
                hintText:
                    'The end-to-end objective the team collaborates on.',
              ),
            ),
            const SizedBox(height: 12),
            Text('TEAM',
                style: TextStyle(
                  color: Fleet.ink400,
                  fontSize: 10,
                  fontWeight: FontWeight.w700,
                  letterSpacing: 0.7,
                )),
            const SizedBox(height: 6),
            if (instances.isEmpty)
              Text(
                'No bots on this fleet yet. A swarm runs on real instances, '
                'so provision one first.',
                style: TextStyle(color: Fleet.ink400, fontSize: 12),
              )
            else
              Container(
                constraints: const BoxConstraints(maxHeight: 240),
                decoration: BoxDecoration(
                  color: Fleet.ink950,
                  borderRadius: BorderRadius.circular(12),
                  border: Border.all(color: Fleet.ink700),
                ),
                child: ListView(
                  shrinkWrap: true,
                  padding: const EdgeInsets.symmetric(vertical: 4),
                  children: [
                    for (final inst in instances) _memberRow(inst),
                  ],
                ),
              ),
            const SizedBox(height: 4),
            Text(
              'Each member is given the mission, its own role, and the names '
              'of its teammates, then starts a task straight away.',
              style: TextStyle(color: Fleet.ink400, fontSize: 11, height: 1.4),
            ),
            InlineError(_error),
            const SizedBox(height: 14),
            FilledButton(
              onPressed: _busy || !_ready ? null : _launch,
              child: _busy
                  ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2))
                  : const Text('Launch swarm'),
            ),
          ],
        ),
      ),
    );
  }

  Widget _memberRow(Instance inst) {
    final picked = _roles.containsKey(inst.id);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        CheckboxListTile(
          dense: true,
          value: picked,
          controlAffinity: ListTileControlAffinity.leading,
          contentPadding: const EdgeInsets.symmetric(horizontal: 8),
          title: Text(inst.name,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(fontSize: 13)),
          secondary: StateChip(state: inst.state, live: inst.isRunning),
          onChanged: _busy
              ? null
              : (v) => setState(() {
                    if (v == true) {
                      // Seeded from the archetype, because that is usually
                      // what the bot is for; still editable below.
                      _roles[inst.id] = inst.archetypeId.isEmpty
                          ? 'Contributor'
                          : inst.archetypeId;
                    } else {
                      _roles.remove(inst.id);
                    }
                  }),
        ),
        if (picked)
          Padding(
            padding: const EdgeInsets.fromLTRB(48, 0, 12, 10),
            child: TextFormField(
              key: ValueKey('role-${inst.id}'),
              initialValue: _roles[inst.id],
              enabled: !_busy,
              style: const TextStyle(fontSize: 12),
              decoration: const InputDecoration(
                isDense: true,
                labelText: 'Role on this mission',
              ),
              onChanged: (v) => _roles[inst.id] = v,
            ),
          ),
      ],
    );
  }
}
