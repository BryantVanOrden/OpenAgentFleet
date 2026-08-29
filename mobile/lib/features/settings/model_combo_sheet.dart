import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';

/// Build a combination: which model does what.
///
/// Simple is a brain and a pair of hands — the split that matters most, since
/// reading a screen and reasoning about it reward completely different models.
/// Advanced exposes every role for when summarising a long thread should not
/// cost what planning does.
class ModelComboSheet extends ConsumerStatefulWidget {
  const ModelComboSheet({super.key, this.existing});

  final ModelCombo? existing;

  static Future<bool?> show(BuildContext context, {ModelCombo? existing}) =>
      showModalBottomSheet<bool>(
        context: context,
        isScrollControlled: true,
        builder: (_) => ModelComboSheet(existing: existing),
      );

  @override
  ConsumerState<ModelComboSheet> createState() => _ModelComboSheetState();
}

class _ModelComboSheetState extends ConsumerState<ModelComboSheet> {
  late final _name = TextEditingController(text: widget.existing?.name ?? '');
  late final _description =
      TextEditingController(text: widget.existing?.description ?? '');

  late final Map<String, String> _roles = {...?widget.existing?.roles};
  late bool _advanced = !(widget.existing?.isSimple ?? true);

  List<AIProvider> _providers = const [];
  bool _loading = true;
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _name.dispose();
    _description.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final list = await ref.read(apiProvider).providers();
      if (!mounted) return;
      setState(() {
        _providers = list;
        _loading = false;
      });
    } catch (err) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = '$err';
      });
    }
  }

  /// Roles on offer. Simple mode hides everything but the two that matter.
  List<String> get _visibleRoles => _advanced
      ? ModelCombo.allRoles
      : const [ModelCombo.roleVision, ModelCombo.roleReasoning];

  /// The hands must be able to see. A text-only model here produces an agent
  /// that is skipped on every turn carrying a screenshot, which looks exactly
  /// like an agent doing nothing.
  List<AIProvider> _choicesFor(String role) => role == ModelCombo.roleVision
      ? _providers.where((p) => p.vision).toList()
      : _providers;

  Future<void> _save() async {
    final roles = Map<String, String>.from(_roles)
      ..removeWhere((k, v) => v.isEmpty || !_visibleRoles.contains(k));
    if (_name.text.trim().isEmpty) {
      setState(() => _error = 'Give the combination a name.');
      return;
    }
    if (roles.isEmpty) {
      setState(() => _error = 'Assign at least one role.');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    final navigator = Navigator.of(context);
    try {
      await ref.read(apiProvider).saveModelCombo(ModelCombo(
            id: widget.existing?.id ?? '',
            name: _name.text.trim(),
            description: _description.text.trim(),
            roles: roles,
          ));
      navigator.pop(true);
    } catch (err) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _error = '$err';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: EdgeInsets.only(
        left: 16,
        right: 16,
        top: 16,
        bottom: MediaQuery.of(context).viewInsets.bottom + 16,
      ),
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(widget.existing == null ? 'New combination' : 'Edit combination',
                style:
                    const TextStyle(fontSize: 16, fontWeight: FontWeight.w700)),
            const SizedBox(height: 4),
            Text(
              'Send each kind of thinking to the model suited to it.',
              style: TextStyle(color: Fleet.ink400, fontSize: 11.5),
            ),
            const SizedBox(height: 14),
            TextField(
              controller: _name,
              decoration: const InputDecoration(
                labelText: 'Name',
                hintText: 'e.g. Fast eyes, deep brain',
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _description,
              decoration: const InputDecoration(
                labelText: 'What it is for (optional)',
              ),
            ),
            const SizedBox(height: 14),
            SegmentedButton<bool>(
              segments: const [
                ButtonSegment(value: false, label: Text('Simple')),
                ButtonSegment(value: true, label: Text('Advanced')),
              ],
              selected: {_advanced},
              onSelectionChanged: (v) => setState(() => _advanced = v.first),
              showSelectedIcon: false,
            ),
            const SizedBox(height: 6),
            Text(
              _advanced
                  ? 'Every role separately. Anything you leave unset falls back '
                      'within this combination before the chain moves on.'
                  : 'A brain and a pair of hands. The other roles follow them.',
              style: TextStyle(color: Fleet.ink400, fontSize: 11, height: 1.4),
            ),
            const SizedBox(height: 14),
            if (_loading)
              const Padding(
                padding: EdgeInsets.all(20),
                child: Center(child: CircularProgressIndicator()),
              )
            else if (_providers.isEmpty)
              Text('No AI connections yet — add one first.',
                  style: TextStyle(color: Fleet.warn, fontSize: 12))
            else
              for (final role in _visibleRoles) _roleRow(role),
            InlineError(_error),
            const SizedBox(height: 14),
            SizedBox(
              width: double.infinity,
              child: FilledButton(
                onPressed: _busy ? null : _save,
                child: _busy
                    ? const SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(strokeWidth: 2))
                    : const Text('Save'),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _roleRow(String role) {
    final choices = _choicesFor(role);
    final selected = _roles[role] ?? '';

    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(ModelCombo.roleLabels[role] ?? role,
              style: TextStyle(
                  color: Fleet.ink300,
                  fontSize: 11.5,
                  fontWeight: FontWeight.w600)),
          const SizedBox(height: 5),
          if (choices.isEmpty)
            Text(
              role == ModelCombo.roleVision
                  ? 'No connection can see the screen — add one with vision.'
                  : 'No connections available.',
              style: TextStyle(color: Fleet.warn, fontSize: 11),
            )
          else
            Wrap(
              spacing: 6,
              runSpacing: 6,
              children: [
                ChoiceChip(
                  label: const Text('Unset', style: TextStyle(fontSize: 11)),
                  selected: selected.isEmpty,
                  onSelected: (_) => setState(() => _roles.remove(role)),
                ),
                for (final p in choices)
                  ChoiceChip(
                    label: Text('${p.name} · ${p.model}',
                        style: const TextStyle(fontSize: 11)),
                    selected: selected == p.id,
                    onSelected: (_) => setState(() => _roles[role] = p.id),
                  ),
              ],
            ),
        ],
      ),
    );
  }
}
