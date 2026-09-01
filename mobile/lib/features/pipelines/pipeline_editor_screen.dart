import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';
import 'pipeline_draft.dart';

/// The graph builder. A full screen rather than a sheet: a pipeline is a name,
/// N stages and M dependencies, each with several fields, and a bottom sheet
/// runs out of room at the second stage.
///
/// This is a form, not a canvas. Dragging boxes around would need a layout
/// engine and a lot of pixels to express the three facts a stage actually has
/// — what to run, where to run it, and what it waits for — and the condition
/// on a dependency is the part that matters most and is hardest to read off a
/// diagram. The DAG view on the pipelines screen draws the result.
class PipelineEditorScreen extends ConsumerStatefulWidget {
  const PipelineEditorScreen({super.key, this.existing});

  /// Null builds a new pipeline; a pipeline edits it in place.
  final WorkflowPipeline? existing;

  @override
  ConsumerState<PipelineEditorScreen> createState() =>
      _PipelineEditorScreenState();
}

class _PipelineEditorScreenState extends ConsumerState<PipelineEditorScreen> {
  late final PipelineDraft _draft = widget.existing == null
      ? PipelineDraft()
      : PipelineDraft.from(widget.existing!);

  bool _showAdvanced = false;
  bool _saving = false;
  String? _error;

  Future<void> _save() async {
    if (validateDraft(_draft).isNotEmpty) return;
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await ref.read(apiProvider).savePipeline(
            // Present on an edit, absent on a create: the server keeps the id
            // when one is supplied, so editing updates in place rather than
            // leaving a duplicate behind.
            id: widget.existing?.id ?? '',
            name: _draft.name.trim(),
            description: _draft.description.trim(),
            nodes: [for (final n in _draft.nodes) n.toJson()],
            edges: [for (final e in _draft.edges) e.toJson()],
            maxParallel: _draft.maxParallel,
          );
      if (mounted) Navigator.pop(context, true);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final instances =
        ref.watch(instancesProvider).valueOrNull ?? const <Instance>[];
    // The archetype list comes from the instances that exist, because a role
    // with no bot to run it is a stage that can never be scheduled.
    final archetypes = {
      for (final i in instances)
        if (i.archetypeId.isNotEmpty) i.archetypeId,
    }.toList()
      ..sort();

    final problems = validateDraft(_draft);

    return Scaffold(
      appBar: AppBar(
        title: Text(widget.existing == null ? 'New pipeline' : 'Edit pipeline'),
      ),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
        children: [
          TextFormField(
            initialValue: _draft.name,
            decoration: const InputDecoration(
              labelText: 'Pipeline name',
              hintText: 'e.g. Nightly security patch and QA',
            ),
            onChanged: (v) => setState(() => _draft.name = v),
          ),
          const SizedBox(height: 12),
          TextFormField(
            initialValue: _draft.description,
            decoration: const InputDecoration(
              labelText: 'Description (optional)',
              hintText: 'What this workflow delivers',
            ),
            onChanged: (v) => setState(() => _draft.description = v),
          ),
          const SizedBox(height: 20),
          _sectionHeader(
            'Stages (${_draft.nodes.length})',
            action: TextButton.icon(
              onPressed: () => setState(
                  () => _draft.nodes.add(DraftNode(id: _draft.nextNodeId()))),
              icon: const Icon(Icons.add, size: 16),
              label: const Text('Add stage'),
            ),
          ),
          for (var i = 0; i < _draft.nodes.length; i++)
            _StageCard(
              key: ValueKey(_draft.nodes[i].id),
              node: _draft.nodes[i],
              archetypes: archetypes,
              instances: instances,
              // A pipeline must have at least one stage, so the last one
              // cannot be removed.
              onRemove: _draft.nodes.length <= 1
                  ? null
                  : () => setState(() {
                        final gone = _draft.nodes.removeAt(i).id;
                        // Edges touching a removed stage would fail validation
                        // server-side with a message about a stage no longer on
                        // screen, so they go too.
                        _draft.edges.removeWhere((e) =>
                            e.fromNodeId == gone || e.toNodeId == gone);
                      }),
              onChanged: () => setState(() {}),
            ),
          const SizedBox(height: 20),
          _sectionHeader(
            'Dependencies (${_draft.edges.length})',
            action: TextButton.icon(
              onPressed: _draft.nodes.length < 2
                  ? null
                  : () => setState(() => _draft.edges.add(DraftEdge(
                        fromNodeId: _draft.nodes[0].id,
                        toNodeId: _draft.nodes[1].id,
                      ))),
              icon: const Icon(Icons.add, size: 16),
              label: const Text('Add dependency'),
            ),
          ),
          Text(
            'Stages with no dependency start together, up to the parallel limit.',
            style: TextStyle(color: Fleet.ink500, fontSize: 11),
          ),
          if (_draft.edges.isEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 10),
              child: Text('No dependencies: every stage runs in parallel.',
                  style: TextStyle(
                      color: Fleet.ink400,
                      fontSize: 12,
                      fontStyle: FontStyle.italic)),
            ),
          for (var i = 0; i < _draft.edges.length; i++)
            _EdgeCard(
              key: ValueKey('edge-$i'),
              edge: _draft.edges[i],
              nodes: _draft.nodes,
              onRemove: () => setState(() => _draft.edges.removeAt(i)),
              onChanged: () => setState(() {}),
            ),
          const SizedBox(height: 16),
          TextButton.icon(
            style: TextButton.styleFrom(
              foregroundColor: Fleet.ink300,
              alignment: Alignment.centerLeft,
            ),
            onPressed: () => setState(() => _showAdvanced = !_showAdvanced),
            icon: Icon(
              _showAdvanced ? Icons.arrow_drop_down : Icons.arrow_right,
              size: 20,
            ),
            label: const Text('Advanced'),
          ),
          if (_showAdvanced)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: TextFormField(
                initialValue: '${_draft.maxParallel}',
                keyboardType: TextInputType.number,
                decoration: const InputDecoration(
                  labelText:
                      'Maximum stages running at once (0 for the default of 4)',
                ),
                onChanged: (v) => setState(() =>
                    _draft.maxParallel = (int.tryParse(v) ?? 0).clamp(0, 32)),
              ),
            ),
          if (problems.isNotEmpty) ...[
            const SizedBox(height: 16),
            Container(
              width: double.infinity,
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                color: Fleet.warn.withValues(alpha: 0.1),
                borderRadius: BorderRadius.circular(10),
                border: Border.all(color: Fleet.warn.withValues(alpha: 0.4)),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('This pipeline cannot be saved yet:',
                      style: TextStyle(
                          color: Fleet.warn,
                          fontSize: 12,
                          fontWeight: FontWeight.w600)),
                  const SizedBox(height: 4),
                  for (final p in problems)
                    Padding(
                      padding: const EdgeInsets.only(top: 2),
                      child: Text('• $p',
                          style: TextStyle(
                              color: Fleet.warn, fontSize: 11.5, height: 1.3)),
                    ),
                ],
              ),
            ),
          ],
          InlineError(_error),
          const SizedBox(height: 16),
          FilledButton.icon(
            onPressed: _saving || problems.isNotEmpty ? null : _save,
            icon: _saving
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.save_outlined),
            label: Text(_saving
                ? 'Saving...'
                : widget.existing == null
                    ? 'Create pipeline'
                    : 'Save changes'),
          ),
        ],
      ),
    );
  }

  Widget _sectionHeader(String label, {Widget? action}) => Row(
        children: [
          Expanded(
            child: Text(label.toUpperCase(),
                style: TextStyle(
                    color: Fleet.ink400,
                    fontSize: 10,
                    fontWeight: FontWeight.w700,
                    letterSpacing: 0.7)),
          ),
          if (action != null) action,
        ],
      );
}

class _StageCard extends StatelessWidget {
  const _StageCard({
    super.key,
    required this.node,
    required this.archetypes,
    required this.instances,
    required this.onRemove,
    required this.onChanged,
  });

  final DraftNode node;
  final List<String> archetypes;
  final List<Instance> instances;
  final VoidCallback? onRemove;
  final VoidCallback onChanged;

  @override
  Widget build(BuildContext context) {
    // One control for both fields. A stage names an instance or an archetype,
    // never both, and two separate pickers let an operator fill in each of
    // them and then wonder which one won.
    final runOn = node.instanceId.isNotEmpty
        ? 'i:${node.instanceId}'
        : 'a:${node.archetypeId}';

    return Container(
      margin: const EdgeInsets.only(top: 10),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Fleet.ink950,
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: Fleet.ink800),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Container(
                padding:
                    const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                decoration: BoxDecoration(
                  color: Fleet.ink800,
                  borderRadius: BorderRadius.circular(5),
                ),
                child: Text(node.id,
                    style: TextStyle(
                        fontFamily: 'monospace',
                        fontSize: 10,
                        color: Fleet.ink300)),
              ),
              const Spacer(),
              TextButton(
                onPressed: onRemove,
                child: const Text('Remove', style: TextStyle(fontSize: 12)),
              ),
            ],
          ),
          const SizedBox(height: 8),
          TextFormField(
            initialValue: node.name,
            decoration: const InputDecoration(
              labelText: 'Stage name',
              hintText: 'e.g. Security audit',
              isDense: true,
            ),
            onChanged: (v) {
              node.name = v;
              onChanged();
            },
          ),
          const SizedBox(height: 10),
          DropdownButtonFormField<String>(
            initialValue: runOn,
            isExpanded: true,
            decoration: const InputDecoration(labelText: 'Run on', isDense: true),
            dropdownColor: Fleet.ink850,
            items: [
              const DropdownMenuItem(
                  value: 'a:', child: Text('— pick a bot or a role —')),
              if (archetypes.isNotEmpty)
                DropdownMenuItem(
                  enabled: false,
                  value: '_h1',
                  child: Text('ANY FREE BOT WITH THIS ROLE',
                      style: TextStyle(
                          color: Fleet.ink500,
                          fontSize: 10,
                          fontWeight: FontWeight.w700,
                          letterSpacing: 0.6)),
                ),
              for (final a in archetypes)
                DropdownMenuItem(value: 'a:$a', child: Text(a)),
              if (instances.isNotEmpty)
                DropdownMenuItem(
                  enabled: false,
                  value: '_h2',
                  child: Text('THIS SPECIFIC BOT',
                      style: TextStyle(
                          color: Fleet.ink500,
                          fontSize: 10,
                          fontWeight: FontWeight.w700,
                          letterSpacing: 0.6)),
                ),
              for (final i in instances)
                DropdownMenuItem(
                  value: 'i:${i.id}',
                  child: Text('${i.name} (${i.state})',
                      overflow: TextOverflow.ellipsis),
                ),
              // Keep a stage referencing a bot that has since been destroyed
              // selectable rather than crashing the dropdown.
              if (node.instanceId.isNotEmpty &&
                  !instances.any((i) => i.id == node.instanceId))
                DropdownMenuItem(
                  value: 'i:${node.instanceId}',
                  child: Text('${node.instanceId} (gone)',
                      overflow: TextOverflow.ellipsis),
                ),
              if (node.archetypeId.isNotEmpty &&
                  !archetypes.contains(node.archetypeId))
                DropdownMenuItem(
                    value: 'a:${node.archetypeId}',
                    child: Text(node.archetypeId)),
            ],
            onChanged: (v) {
              if (v == null || v.startsWith('_')) return;
              final value = v.substring(2);
              if (v.startsWith('i:')) {
                node.instanceId = value;
                node.archetypeId = '';
              } else {
                node.archetypeId = value;
                node.instanceId = '';
              }
              onChanged();
            },
          ),
          const SizedBox(height: 10),
          TextFormField(
            initialValue: node.goalTemplate,
            minLines: 2,
            maxLines: 4,
            style: const TextStyle(fontSize: 13, height: 1.35),
            decoration: const InputDecoration(
              labelText: 'Goal',
              hintText: 'What this bot is asked to do. '
                  '{{payload}} interpolates the trigger body.',
              isDense: true,
            ),
            onChanged: (v) {
              node.goalTemplate = v;
              onChanged();
            },
          ),
        ],
      ),
    );
  }
}

class _EdgeCard extends StatelessWidget {
  const _EdgeCard({
    super.key,
    required this.edge,
    required this.nodes,
    required this.onRemove,
    required this.onChanged,
  });

  final DraftEdge edge;
  final List<DraftNode> nodes;
  final VoidCallback onRemove;
  final VoidCallback onChanged;

  @override
  Widget build(BuildContext context) {
    final c = splitCondition(edge.condition);
    final needsArg = c.kind.endsWith(':');

    DropdownButtonFormField<String> nodePicker(
        String label, String value, void Function(String) set) {
      return DropdownButtonFormField<String>(
        initialValue: nodes.any((n) => n.id == value) ? value : null,
        isExpanded: true,
        decoration: InputDecoration(labelText: label, isDense: true),
        dropdownColor: Fleet.ink850,
        items: [
          for (final n in nodes)
            DropdownMenuItem(
              value: n.id,
              child: Text(n.name.trim().isEmpty ? n.id : n.name,
                  overflow: TextOverflow.ellipsis),
            ),
        ],
        onChanged: (v) {
          if (v == null) return;
          set(v);
          onChanged();
        },
      );
    }

    return Container(
      margin: const EdgeInsets.only(top: 10),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Fleet.ink950,
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: Fleet.ink800),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              Expanded(
                child: nodePicker(
                    'After', edge.fromNodeId, (v) => edge.fromNodeId = v),
              ),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 6),
                child:
                    Icon(Icons.arrow_forward, size: 16, color: Fleet.ink500),
              ),
              Expanded(
                child:
                    nodePicker('Run', edge.toNodeId, (v) => edge.toNodeId = v),
              ),
            ],
          ),
          const SizedBox(height: 10),
          DropdownButtonFormField<String>(
            initialValue: c.kind,
            isExpanded: true,
            decoration:
                const InputDecoration(labelText: 'Condition', isDense: true),
            dropdownColor: Fleet.ink850,
            items: [
              for (final opt in edgeConditions)
                DropdownMenuItem(value: opt.value, child: Text(opt.label)),
            ],
            onChanged: (v) {
              if (v == null) return;
              // Keep the typed argument when switching between two conditions
              // that both take one.
              edge.condition = v.endsWith(':') ? '$v${c.arg}' : v;
              onChanged();
            },
          ),
          if (needsArg) ...[
            const SizedBox(height: 10),
            TextFormField(
              // Keyed on the condition kind so switching kinds rebuilds the
              // field with the preserved argument rather than a stale one.
              key: ValueKey('arg-${c.kind}'),
              initialValue: c.arg,
              style: const TextStyle(fontFamily: 'monospace', fontSize: 13),
              decoration: InputDecoration(
                labelText:
                    c.kind == 'matches:' ? 'Regular expression' : 'Text',
                hintText:
                    c.kind == 'matches:' ? r'^\d+ tests passed' : 'approved',
                isDense: true,
              ),
              onChanged: (v) {
                edge.condition = '${c.kind}$v';
                onChanged();
              },
            ),
          ],
          Align(
            alignment: Alignment.centerRight,
            child: TextButton(
              onPressed: onRemove,
              child: const Text('Remove', style: TextStyle(fontSize: 12)),
            ),
          ),
        ],
      ),
    );
  }
}
