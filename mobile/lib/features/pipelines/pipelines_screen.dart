import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import 'pipeline_editor_screen.dart';
import 'schedules_screen.dart';

class PipelinesScreen extends ConsumerStatefulWidget {
  const PipelinesScreen({super.key});

  @override
  ConsumerState<PipelinesScreen> createState() => _PipelinesScreenState();
}

class _PipelinesScreenState extends ConsumerState<PipelinesScreen> {
  List<WorkflowPipeline> _pipelines = [];
  bool _loading = false;
  String? _error;

  /// Bumped on every reload so the runs panels re-fetch alongside the list.
  int _reloadTick = 0;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final api = ref.read(apiProvider);
      final list = await api.pipelines();
      if (mounted) {
        setState(() {
          _pipelines = list;
          _reloadTick++;
        });
      }
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _openEditor([WorkflowPipeline? existing]) async {
    final saved = await Navigator.of(context).push<bool>(
      MaterialPageRoute(
          builder: (_) => PipelineEditorScreen(existing: existing)),
    );
    if (saved == true) _load();
  }

  Future<void> _delete(WorkflowPipeline p) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Delete "${p.name}"?'),
        content: Text(
          'The pipeline and its run history are removed. Anything scheduled '
          'to trigger it will have nothing to run.',
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
      await ref.read(apiProvider).deletePipeline(p.id);
      await _load();
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    }
  }

  Future<void> _trigger(WorkflowPipeline p) async {
    try {
      final api = ref.read(apiProvider);
      final run = await api.runPipeline(p.id);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('🚀 Triggered pipeline run: ${run.id} (${p.name})'),
            backgroundColor: Fleet.live,
          ),
        );
      }
    } catch (err) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Error: $err'), backgroundColor: Fleet.bad),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    // Reload when this tab is opened. The shell keeps every tab alive in an
    // IndexedStack, so loading in initState alone meant showing whatever was
    // fetched when the app started, for the rest of the session.
    ref.listen(tabRefreshProvider(Tabs.pipelines), (_, __) => _load());

    return Scaffold(
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => _openEditor(),
        icon: const Icon(Icons.add),
        label: const Text('New pipeline'),
      ),
      appBar: AppBar(
        title: const Text('Pipelines'),
        actions: [
          // Schedules are the other half of "what runs without me": a pipeline
          // is the shape of the work, a cron trigger is when it wakes up.
          IconButton(
            tooltip: 'Schedules & wakeups',
            icon: const Icon(Icons.schedule),
            onPressed: () => Navigator.of(context).push(
              MaterialPageRoute(builder: (_) => const SchedulesScreen()),
            ),
          ),
          IconButton(
            icon: const Icon(Icons.refresh),
            onPressed: _load,
          ),
        ],
      ),
      body: _loading && _pipelines.isEmpty
          ? const Center(child: CircularProgressIndicator())
          : _error != null && _pipelines.isEmpty
              ? Center(child: Text(_error!, style: TextStyle(color: Fleet.bad)))
              : _pipelines.isEmpty
                  ? Center(
                  child: Padding(
                    padding: const EdgeInsets.all(24),
                    child: Column(
                      mainAxisAlignment: MainAxisAlignment.center,
                      children: [
                        Icon(Icons.account_tree_outlined, size: 48, color: Fleet.ink400),
                        const SizedBox(height: 12),
                        const Text(
                          'No workflow pipelines found.',
                          style: TextStyle(fontWeight: FontWeight.bold),
                        ),
                        const SizedBox(height: 4),
                        Text(
                          'A pipeline is a set of stages and the dependencies '
                          'between them. Each stage starts a real task on a '
                          'real bot.',
                          textAlign: TextAlign.center,
                          style: TextStyle(color: Fleet.ink400, fontSize: 12),
                        ),
                      ],
                    ),
                  ),
                )
              : ListView.builder(
                  padding: const EdgeInsets.fromLTRB(16, 16, 16, 96),
                  itemCount: _pipelines.length,
                  itemBuilder: (ctx, i) {
                    final p = _pipelines[i];
                    return Card(
                      color: Fleet.ink900,
                      margin: const EdgeInsets.only(bottom: 12),
                      shape: RoundedRectangleBorder(
                        borderRadius: BorderRadius.circular(12),
                        side: BorderSide(color: Fleet.ink800),
                      ),
                      child: Padding(
                        padding: const EdgeInsets.all(16),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Row(
                              mainAxisAlignment: MainAxisAlignment.spaceBetween,
                              children: [
                                Expanded(
                                  child: Text(
                                    p.name,
                                    style: const TextStyle(
                                        fontSize: 16, fontWeight: FontWeight.bold),
                                  ),
                                ),
                                Container(
                                  padding: const EdgeInsets.symmetric(
                                      horizontal: 8, vertical: 2),
                                  decoration: BoxDecoration(
                                    color: Fleet.ink800,
                                    borderRadius: BorderRadius.circular(6),
                                  ),
                                  child: Text(
                                    '${p.nodes.length} STAGES',
                                    style: TextStyle(
                                        fontSize: 10,
                                        fontFamily: 'monospace',
                                        color: Fleet.live),
                                  ),
                                ),
                              ],
                            ),
                            if (p.description.isNotEmpty) ...[
                              const SizedBox(height: 4),
                              Text(p.description,
                                  style: TextStyle(color: Fleet.ink400, fontSize: 12)),
                            ],
                            const SizedBox(height: 12),
                            // The graph, laid out by dependency depth. A flat
                            // chip list implied every pipeline was a straight
                            // line regardless of what its edges actually said.
                            _DagView(pipeline: p),
                            const SizedBox(height: 12),
                            _RunsPanel(
                              pipeline: p,
                              reloadTick: _reloadTick,
                            ),
                            const SizedBox(height: 12),
                            Row(
                              children: [
                                Expanded(
                                  child: FilledButton.icon(
                                    onPressed: () => _trigger(p),
                                    icon: const Icon(Icons.play_arrow, size: 18),
                                    label: const Text('Run Pipeline'),
                                  ),
                                ),
                                const SizedBox(width: 8),
                                IconButton(
                                  tooltip: 'Edit',
                                  icon: const Icon(Icons.edit_outlined, size: 20),
                                  onPressed: () => _openEditor(p),
                                ),
                                IconButton(
                                  tooltip: 'Delete',
                                  icon: Icon(Icons.delete_outline,
                                      size: 20, color: Fleet.bad),
                                  onPressed: () => _delete(p),
                                ),
                              ],
                            ),
                          ],
                        ),
                      ),
                    );
                  },
                ),
    );
  }
}

/// The pipeline's runs: status dot, id, and a per-state node tally — several
/// stages genuinely run at once, so a single "current node" cannot describe a
/// run. Fetched per pipeline and re-fetched whenever the screen reloads.
class _RunsPanel extends ConsumerStatefulWidget {
  const _RunsPanel({required this.pipeline, required this.reloadTick});

  final WorkflowPipeline pipeline;
  final int reloadTick;

  @override
  ConsumerState<_RunsPanel> createState() => _RunsPanelState();
}

class _RunsPanelState extends ConsumerState<_RunsPanel> {
  List<PipelineRun> _runs = const [];
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void didUpdateWidget(covariant _RunsPanel old) {
    super.didUpdateWidget(old);
    // Rides the screen's own reload — refresh button, tab arrival, a run just
    // triggered — rather than adding a second polling loop of its own.
    if (old.reloadTick != widget.reloadTick ||
        old.pipeline.id != widget.pipeline.id) {
      _load();
    }
  }

  Future<void> _load() async {
    try {
      final runs = await ref.read(apiProvider).pipelineRuns(widget.pipeline.id);
      runs.sort((a, b) => b.startedAt.compareTo(a.startedAt));
      if (mounted) {
        setState(() {
          _runs = runs;
          _error = null;
        });
      }
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    }
  }

  Color _statusColor(String status) => switch (status) {
        'running' => Fleet.live,
        'completed' => Fleet.good,
        'cancelled' => Fleet.ink500,
        _ => Fleet.bad,
      };

  @override
  Widget build(BuildContext context) {
    if (_error != null) {
      return Text('Could not load runs: $_error',
          style: TextStyle(color: Fleet.bad, fontSize: 11));
    }
    if (_runs.isEmpty) {
      return Text('This pipeline has not run yet.',
          style: TextStyle(
              color: Fleet.ink400, fontSize: 11, fontStyle: FontStyle.italic));
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('RUNS',
            style: TextStyle(
                color: Fleet.ink400,
                fontSize: 10,
                fontWeight: FontWeight.w700,
                letterSpacing: 0.7)),
        for (final r in _runs.take(5))
          Container(
            margin: const EdgeInsets.only(top: 6),
            padding: const EdgeInsets.all(10),
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
                    Container(
                      width: 8,
                      height: 8,
                      decoration: BoxDecoration(
                        color: _statusColor(r.status),
                        shape: BoxShape.circle,
                      ),
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(r.id,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                              fontFamily: 'monospace',
                              fontSize: 11,
                              color: Fleet.ink200)),
                    ),
                    Container(
                      padding: const EdgeInsets.symmetric(
                          horizontal: 6, vertical: 1),
                      decoration: BoxDecoration(
                        color: Fleet.ink800,
                        borderRadius: BorderRadius.circular(4),
                      ),
                      child: Text(r.status.toUpperCase(),
                          style:
                              TextStyle(fontSize: 9, color: Fleet.ink300)),
                    ),
                  ],
                ),
                const SizedBox(height: 4),
                Text(
                  'Started ${humanAgo(r.startedAt)}'
                  '${r.stateTally.entries.map((e) => ' · ${e.value} ${e.key}').join()}',
                  style: TextStyle(color: Fleet.ink400, fontSize: 10),
                ),
              ],
            ),
          ),
      ],
    );
  }
}

/// Renders a pipeline as dependency layers.
///
/// Stages on the same row have no dependency between them and run in
/// parallel, bounded by the pipeline's max_parallel; each row waits on the
/// one above. Edge conditions are real branches: a stage whose condition is
/// not met is skipped, and the skip propagates downstream. This view used to
/// caption conditions "not enforced" and rows "one at a time" — true when it
/// was written, and then the engine was rebuilt while the caption stayed. A
/// screen understating what the product does is the same defect as one
/// overstating it: the reader plans around a lie either way.
class _DagView extends StatelessWidget {
  const _DagView({required this.pipeline});
  final WorkflowPipeline pipeline;

  @override
  Widget build(BuildContext context) {
    final layers = pipeline.layers;
    if (layers.isEmpty) {
      return Text('No stages defined.',
          style: TextStyle(color: Fleet.ink400, fontSize: 12));
    }

    final conditions = <String, String>{
      for (final e in pipeline.edges)
        if (e.condition.isNotEmpty) e.toNodeId: e.condition,
    };

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (var i = 0; i < layers.length; i++) ...[
          if (i > 0)
            Padding(
              padding: const EdgeInsets.only(left: 10, top: 2, bottom: 2),
              child: Icon(Icons.arrow_downward_rounded,
                  size: 14, color: Fleet.ink600),
            ),
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              SizedBox(
                width: 22,
                child: Text('${i + 1}',
                    style: TextStyle(color: Fleet.ink600, fontSize: 11)),
              ),
              Expanded(
                child: Wrap(
                  spacing: 6,
                  runSpacing: 6,
                  children: [
                    for (final n in layers[i])
                      Container(
                        padding: const EdgeInsets.symmetric(
                            horizontal: 10, vertical: 7),
                        decoration: BoxDecoration(
                          color: Fleet.ink950,
                          borderRadius: BorderRadius.circular(8),
                          border: Border.all(color: Fleet.ink800),
                        ),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(n.name,
                                style: const TextStyle(
                                    fontSize: 12,
                                    fontWeight: FontWeight.w600)),
                            Text(
                                n.instanceId.isNotEmpty
                                    ? 'pinned bot'
                                    : (n.archetypeId.isEmpty
                                        ? 'unassigned'
                                        : n.archetypeId),
                                style: TextStyle(
                                    color: Fleet.ink400, fontSize: 10)),
                            if (conditions[n.id] != null)
                              Padding(
                                padding: const EdgeInsets.only(top: 3),
                                child: Text('runs on ${conditions[n.id]}',
                                    style: TextStyle(
                                        color: Fleet.ink400, fontSize: 9)),
                              ),
                          ],
                        ),
                      ),
                  ],
                ),
              ),
            ],
          ),
        ],
        if (layers.length > 1)
          Padding(
            padding: const EdgeInsets.only(top: 6),
            child: Text(
              'Rows run top to bottom; stages sharing a row run in parallel. '
              'A stage whose condition is not met is skipped.',
              style: TextStyle(color: Fleet.ink400, fontSize: 10),
            ),
          ),
      ],
    );
  }
}
