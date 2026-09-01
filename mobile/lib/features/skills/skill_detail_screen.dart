import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';

/// The timeline editor for one skill: prune the noise out of a recording,
/// label what the recorder could not, and mark the values that should vary
/// between runs.
class SkillDetailScreen extends ConsumerStatefulWidget {
  const SkillDetailScreen({super.key, required this.skill});

  final Skill skill;

  @override
  ConsumerState<SkillDetailScreen> createState() => _SkillDetailScreenState();
}

class _SkillDetailScreenState extends ConsumerState<SkillDetailScreen> {
  late Skill _skill = widget.skill;
  late final _name = TextEditingController(text: _skill.name);
  late final _description = TextEditingController(text: _skill.description);

  bool _dirty = false;
  bool _saving = false;
  bool _refining = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _description.dispose();
    super.dispose();
  }

  void _markDirty() {
    if (!_dirty) setState(() => _dirty = true);
  }

  Future<void> _save() async {
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      _skill.name = _name.text.trim();
      _skill.description = _description.text.trim();
      final saved = await ref.read(apiProvider).saveSkill(_skill);
      if (mounted) {
        setState(() {
          _skill = saved;
          _dirty = false;
        });
      }
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _refine() async {
    setState(() {
      _refining = true;
      _error = null;
    });
    try {
      final refined = await ref.read(apiProvider).refineSkill(_skill.id);
      if (mounted) {
        setState(() {
          _skill = refined;
          _name.text = refined.name;
          _description.text = refined.description;
          _dirty = false;
        });
      }
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _refining = false);
    }
  }

  Future<void> _delete() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Delete "${_skill.name}"?'),
        content: Text(
          'Agents lose this procedure immediately. There is no undo — the '
          'only way back is recording the demonstration again.',
          style: TextStyle(color: Fleet.ink300),
        ),
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
      await ref.read(apiProvider).deleteSkill(_skill.id);
      if (mounted) Navigator.pop(context);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    }
  }

  void _moveStep(int index, int delta) {
    final target = index + delta;
    if (target < 0 || target >= _skill.steps.length) return;
    setState(() {
      final steps = _skill.steps;
      final tmp = steps[index];
      steps[index] = steps[target];
      steps[target] = tmp;
    });
    _markDirty();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(_skill.name, overflow: TextOverflow.ellipsis),
        actions: [
          IconButton(
            tooltip: 'Delete skill',
            icon: Icon(Icons.delete_outline, color: Fleet.bad),
            onPressed: _delete,
          ),
        ],
      ),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: TextField(
                  controller: _name,
                  style: const TextStyle(
                      fontSize: 16, fontWeight: FontWeight.w600),
                  decoration: const InputDecoration(labelText: 'Name'),
                  onChanged: (_) => _markDirty(),
                ),
              ),
              const SizedBox(width: 10),
              Padding(
                padding: const EdgeInsets.only(top: 14),
                child: Container(
                  padding:
                      const EdgeInsets.symmetric(horizontal: 9, vertical: 3),
                  decoration: BoxDecoration(
                    color: Fleet.live.withValues(alpha: 0.12),
                    borderRadius: BorderRadius.circular(999),
                    border:
                        Border.all(color: Fleet.live.withValues(alpha: 0.25)),
                  ),
                  child: Text('v${_skill.version}',
                      style: TextStyle(
                          fontFamily: 'monospace',
                          fontSize: 11,
                          color: Fleet.live)),
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
          TextField(
            controller: _description,
            minLines: 2,
            maxLines: 4,
            style: const TextStyle(fontSize: 13, height: 1.4),
            decoration: const InputDecoration(
              labelText: 'Description',
              hintText: 'What does this skill accomplish, and when should an '
                  'agent reach for it?',
            ),
            onChanged: (_) => _markDirty(),
          ),
          if (_skill.refinementNotes.isNotEmpty) ...[
            const SizedBox(height: 12),
            Container(
              width: double.infinity,
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                color: Fleet.cool.withValues(alpha: 0.08),
                borderRadius: BorderRadius.circular(10),
                border: Border.all(color: Fleet.cool.withValues(alpha: 0.25)),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Icon(Icons.auto_awesome, size: 14, color: Fleet.cool),
                      const SizedBox(width: 6),
                      Text('Continual refinement notes',
                          style: TextStyle(
                              color: Fleet.cool,
                              fontSize: 12,
                              fontWeight: FontWeight.w600)),
                    ],
                  ),
                  const SizedBox(height: 6),
                  Text(_skill.refinementNotes,
                      style: TextStyle(
                          color: Fleet.ink200, fontSize: 12, height: 1.4)),
                ],
              ),
            ),
          ],
          const SizedBox(height: 12),
          Row(
            children: [
              Expanded(
                child: OutlinedButton.icon(
                  onPressed: _refining || _saving ? null : _refine,
                  icon: _refining
                      ? const SizedBox(
                          width: 14,
                          height: 14,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.auto_fix_high, size: 16),
                  label: Text(_refining ? 'Refining...' : 'AI Refine'),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: FilledButton.icon(
                  onPressed: !_dirty || _saving || _refining ? null : _save,
                  icon: _saving
                      ? const SizedBox(
                          width: 14,
                          height: 14,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.save_outlined, size: 16),
                  label:
                      Text(_saving ? 'Saving...' : (_dirty ? 'Save' : 'Saved')),
                ),
              ),
            ],
          ),
          InlineError(_error),
          const SizedBox(height: 20),
          Text('STEPS (${_skill.steps.length})',
              style: TextStyle(
                  color: Fleet.ink400,
                  fontSize: 10,
                  fontWeight: FontWeight.w700,
                  letterSpacing: 0.7)),
          for (var i = 0; i < _skill.steps.length; i++)
            _StepCard(
              key: ObjectKey(_skill.steps[i]),
              index: i,
              step: _skill.steps[i],
              onChanged: _markDirty,
              onParamNamed: (param) {
                if (param.isNotEmpty && !_skill.params.contains(param)) {
                  setState(() => _skill.params = [..._skill.params, param]);
                }
                _markDirty();
              },
              onMoveUp: i == 0 ? null : () => _moveStep(i, -1),
              onMoveDown:
                  i == _skill.steps.length - 1 ? null : () => _moveStep(i, 1),
              onDelete: () {
                setState(() => _skill.steps.removeAt(i));
                _markDirty();
              },
            ),
          const SizedBox(height: 20),
          Text('COMPILED SKILL.MD',
              style: TextStyle(
                  color: Fleet.ink400,
                  fontSize: 10,
                  fontWeight: FontWeight.w700,
                  letterSpacing: 0.7)),
          const SizedBox(height: 6),
          Text('What the model sees. Regenerated by the server on save.',
              style: TextStyle(color: Fleet.ink500, fontSize: 11)),
          const SizedBox(height: 8),
          Container(
            width: double.infinity,
            padding: const EdgeInsets.all(12),
            constraints: const BoxConstraints(maxHeight: 380),
            decoration: BoxDecoration(
              color: Fleet.ink950,
              borderRadius: BorderRadius.circular(10),
              border: Border.all(color: Fleet.ink800),
            ),
            child: SingleChildScrollView(
              child: SelectableText(
                _skill.markdown.isEmpty ? 'Save to regenerate.' : _skill.markdown,
                style: TextStyle(
                    fontFamily: 'monospace',
                    fontSize: 11,
                    height: 1.45,
                    color: Fleet.ink300),
              ),
            ),
          ),
          if (_skill.params.isNotEmpty) ...[
            const SizedBox(height: 12),
            Text('PARAMETERS',
                style: TextStyle(
                    color: Fleet.ink400,
                    fontSize: 10,
                    fontWeight: FontWeight.w700,
                    letterSpacing: 0.7)),
            const SizedBox(height: 6),
            Wrap(
              spacing: 6,
              runSpacing: 6,
              children: [
                for (final p in _skill.params)
                  Container(
                    padding: const EdgeInsets.symmetric(
                        horizontal: 8, vertical: 3),
                    decoration: BoxDecoration(
                      color: Fleet.cool.withValues(alpha: 0.12),
                      borderRadius: BorderRadius.circular(6),
                    ),
                    child: Text('{{$p}}',
                        style: TextStyle(
                            fontFamily: 'monospace',
                            fontSize: 11,
                            color: Fleet.cool)),
                  ),
              ],
            ),
          ],
        ],
      ),
    );
  }
}

class _StepCard extends StatelessWidget {
  const _StepCard({
    super.key,
    required this.index,
    required this.step,
    required this.onChanged,
    required this.onParamNamed,
    required this.onMoveUp,
    required this.onMoveDown,
    required this.onDelete,
  });

  final int index;
  final SkillStep step;
  final VoidCallback onChanged;
  final ValueChanged<String> onParamNamed;
  final VoidCallback? onMoveUp;
  final VoidCallback? onMoveDown;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.only(top: 8),
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: Fleet.ink850,
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: Fleet.ink800),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 24,
            child: Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text('${index + 1}',
                  textAlign: TextAlign.right,
                  style: TextStyle(
                      fontFamily: 'monospace',
                      fontSize: 11,
                      color: Fleet.ink500)),
            ),
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Wrap(
                  spacing: 6,
                  runSpacing: 4,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: [
                    Container(
                      padding: const EdgeInsets.symmetric(
                          horizontal: 6, vertical: 2),
                      decoration: BoxDecoration(
                        color: Fleet.ink800,
                        borderRadius: BorderRadius.circular(4),
                      ),
                      child: Text(step.kind,
                          style: TextStyle(
                              fontFamily: 'monospace',
                              fontSize: 10,
                              color: Fleet.live)),
                    ),
                    if (step.role.isNotEmpty)
                      Text(step.role,
                          style: TextStyle(
                              fontFamily: 'monospace',
                              fontSize: 10,
                              color: Fleet.ink400)),
                    if (step.window.isNotEmpty)
                      Text('in "${step.window}"',
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style:
                              TextStyle(fontSize: 10, color: Fleet.ink500)),
                    if (step.coordinateOnly)
                      Tooltip(
                        message:
                            'No accessible label was captured, so replay falls '
                            'back to coordinates. Fragile if the layout moves.',
                        triggerMode: TooltipTriggerMode.tap,
                        child: Container(
                          padding: const EdgeInsets.symmetric(
                              horizontal: 6, vertical: 2),
                          decoration: BoxDecoration(
                            color: Fleet.warn.withValues(alpha: 0.12),
                            borderRadius: BorderRadius.circular(4),
                          ),
                          child: Text('coordinate-only',
                              style: TextStyle(
                                  fontSize: 10, color: Fleet.warn)),
                        ),
                      ),
                  ],
                ),
                // Editable even on a coordinate-only step: giving one a label
                // is exactly how it stops being fragile.
                if (step.label != null || step.coordinates.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  TextFormField(
                    initialValue: step.label ?? '',
                    style: const TextStyle(fontSize: 12),
                    decoration: const InputDecoration(
                      hintText: 'accessible label',
                      isDense: true,
                    ),
                    onChanged: (v) {
                      step.label = v;
                      onChanged();
                    },
                  ),
                ],
                if (step.kind == 'type') ...[
                  const SizedBox(height: 8),
                  TextFormField(
                    initialValue: step.text,
                    style: const TextStyle(fontSize: 12),
                    decoration: const InputDecoration(
                      labelText: 'Text typed',
                      isDense: true,
                    ),
                    onChanged: (v) {
                      step.text = v;
                      onChanged();
                    },
                  ),
                  const SizedBox(height: 8),
                  TextFormField(
                    initialValue: step.param,
                    style: const TextStyle(
                        fontFamily: 'monospace', fontSize: 12),
                    decoration: const InputDecoration(
                      labelText: 'Parameter name',
                      hintText: 'names this a fill-in value',
                      isDense: true,
                    ),
                    onChanged: (v) {
                      step.param = v.trim();
                      onParamNamed(v.trim());
                    },
                  ),
                ],
              ],
            ),
          ),
          const SizedBox(width: 4),
          Column(
            children: [
              _MiniButton(icon: Icons.arrow_upward, onPressed: onMoveUp),
              _MiniButton(icon: Icons.arrow_downward, onPressed: onMoveDown),
              _MiniButton(
                  icon: Icons.close, color: Fleet.bad, onPressed: onDelete),
            ],
          ),
        ],
      ),
    );
  }
}

class _MiniButton extends StatelessWidget {
  const _MiniButton({required this.icon, this.onPressed, this.color});

  final IconData icon;
  final VoidCallback? onPressed;
  final Color? color;

  @override
  Widget build(BuildContext context) => IconButton(
        icon: Icon(icon, size: 15, color: color ?? Fleet.ink300),
        padding: EdgeInsets.zero,
        constraints: const BoxConstraints(minWidth: 28, minHeight: 28),
        onPressed: onPressed,
        disabledColor: Fleet.ink700,
      );
}
