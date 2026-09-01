import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';

/// Start a run on one bot: a goal, and optionally a recorded skill giving the
/// agent a procedure to follow rather than working the task out from scratch.
class AssignTaskSheet extends ConsumerStatefulWidget {
  const AssignTaskSheet({super.key, required this.instance});

  final Instance instance;

  /// Returns true when a task was started.
  static Future<bool?> show(BuildContext context, Instance instance) =>
      showModalBottomSheet<bool>(
        context: context,
        isScrollControlled: true,
        backgroundColor: Fleet.ink900,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
        ),
        builder: (ctx) => Padding(
          padding:
              EdgeInsets.only(bottom: MediaQuery.of(ctx).viewInsets.bottom),
          child: AssignTaskSheet(instance: instance),
        ),
      );

  @override
  ConsumerState<AssignTaskSheet> createState() => _AssignTaskSheetState();
}

class _AssignTaskSheetState extends ConsumerState<AssignTaskSheet> {
  final _goal = TextEditingController();
  String _skillId = '';
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _goal.dispose();
    super.dispose();
  }

  Future<void> _start() async {
    final goal = _goal.text.trim();
    if (goal.isEmpty) {
      setState(() => _error = 'Give the agent a goal first.');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(apiProvider).createTask(
            instanceId: widget.instance.id,
            goal: goal,
            skillId: _skillId.isEmpty ? null : _skillId,
          );
      ref.invalidate(tasksProvider(widget.instance.id));
      if (mounted) Navigator.pop(context, true);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final skills = ref.watch(skillsProvider).valueOrNull ?? const <Skill>[];

    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 18, 20, 18),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                const Icon(Icons.add_task, size: 20),
                const SizedBox(width: 8),
                Expanded(
                  child: Text('Assign task — ${widget.instance.name}',
                      style: Theme.of(context).textTheme.titleMedium),
                ),
              ],
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _goal,
              enabled: !_busy,
              minLines: 3,
              maxLines: 6,
              textCapitalization: TextCapitalization.sentences,
              decoration: const InputDecoration(
                labelText: 'Goal',
                hintText:
                    'Pull the latest commit on main and build the release '
                    'target.',
              ),
            ),
            const SizedBox(height: 12),
            DropdownButtonFormField<String>(
              initialValue: _skillId,
              decoration: const InputDecoration(
                labelText: 'Recorded skill',
                helperText:
                    'Optional. Gives the agent a procedure to follow.',
              ),
              dropdownColor: Fleet.ink850,
              items: [
                const DropdownMenuItem(value: '', child: Text('none')),
                for (final s in skills)
                  DropdownMenuItem(
                    value: s.id,
                    child: Text('${s.name} (${s.stepCount} steps)',
                        overflow: TextOverflow.ellipsis),
                  ),
              ],
              onChanged:
                  _busy ? null : (v) => setState(() => _skillId = v ?? ''),
            ),
            InlineError(_error),
            const SizedBox(height: 14),
            FilledButton(
              onPressed: _busy ? null : _start,
              child: _busy
                  ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2))
                  : const Text('Start agent'),
            ),
          ],
        ),
      ),
    );
  }
}
