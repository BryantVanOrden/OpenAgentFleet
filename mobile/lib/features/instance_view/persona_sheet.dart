import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';

/// Edit a bot's personality.
///
/// Every bot used to run on whatever persona its archetype shipped with, and
/// nothing read even that — so a bot asked what it was good at had nothing in
/// context to answer from. The personality is read when a prompt is built
/// rather than baked into the sandbox, so an edit lands on the agent's next
/// turn and its next reply without restarting anything.
class PersonaSheet extends ConsumerStatefulWidget {
  const PersonaSheet({super.key, required this.instance});

  final Instance instance;

  static Future<bool?> show(BuildContext context, Instance instance) =>
      showModalBottomSheet<bool>(
        context: context,
        isScrollControlled: true,
        backgroundColor: Fleet.ink900,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
        ),
        builder: (ctx) => Padding(
          // Lift the sheet above the keyboard: this is a text editor, and a
          // field hidden behind the keyboard is a field you cannot use.
          padding:
              EdgeInsets.only(bottom: MediaQuery.of(ctx).viewInsets.bottom),
          child: PersonaSheet(instance: instance),
        ),
      );

  @override
  ConsumerState<PersonaSheet> createState() => _PersonaSheetState();
}

class _PersonaSheetState extends ConsumerState<PersonaSheet> {
  late final TextEditingController _c =
      TextEditingController(text: widget.instance.systemPrompt);
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref
          .read(apiProvider)
          .setInstancePersona(widget.instance.id, _c.text.trim());
      ref.invalidate(instancesProvider);
      if (mounted) Navigator.pop(context, true);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// Put back what this bot's archetype recommends for the job.
  Future<void> _resetToArchetype() async {
    final templates = ref.read(templatesProvider).valueOrNull ?? const [];
    final t = templates
        .where((t) => t.id == widget.instance.archetypeId)
        .firstOrNull;
    if (t == null) {
      setState(() => _error =
          'This bot has no archetype to take a default personality from.');
      return;
    }
    setState(() => _c.text = t.specializedPrompt);
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 18, 20, 18),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                const Icon(Icons.psychology_outlined, size: 20),
                const SizedBox(width: 8),
                Expanded(
                  child: Text('Personality — ${widget.instance.name}',
                      style: Theme.of(context).textTheme.titleMedium),
                ),
              ],
            ),
            const SizedBox(height: 6),
            Text(
              'How this bot thinks and talks, in your words. It is read every '
              'time the bot answers, so a change applies to its next reply.',
              style: TextStyle(color: Fleet.ink400, fontSize: 12, height: 1.4),
            ),
            const SizedBox(height: 12),
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 280),
              child: TextField(
                controller: _c,
                enabled: !_busy,
                maxLines: null,
                minLines: 6,
                style: const TextStyle(fontSize: 13, height: 1.4),
                decoration: InputDecoration(
                  filled: true,
                  fillColor: Fleet.ink850,
                  hintText:
                      'e.g. Blunt and precise. Leads with the answer, then the '
                      'evidence. Never speculates without saying so.',
                  hintStyle:
                      TextStyle(color: Fleet.ink500, fontSize: 12, height: 1.4),
                  border: OutlineInputBorder(
                    borderRadius: BorderRadius.circular(10),
                    borderSide: BorderSide.none,
                  ),
                ),
              ),
            ),
            if (_error != null) ...[
              const SizedBox(height: 8),
              InlineError(_error!),
            ],
            const SizedBox(height: 12),
            Row(
              children: [
                TextButton.icon(
                  onPressed: _busy ? null : _resetToArchetype,
                  icon: const Icon(Icons.auto_awesome, size: 16),
                  label: const Text('Use the default for its job'),
                ),
                const Spacer(),
                FilledButton(
                  onPressed: _busy ? null : _save,
                  child: _busy
                      ? const SizedBox(
                          width: 16,
                          height: 16,
                          child: CircularProgressIndicator(strokeWidth: 2))
                      : const Text('Save'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
