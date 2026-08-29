import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
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
      if (mounted) setState(() => _pipelines = list);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _loading = false);
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
    return Scaffold(
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
                          'Define multi-bot DAG pipelines in the Admin Console.',
                          textAlign: TextAlign.center,
                          style: TextStyle(color: Fleet.ink400, fontSize: 12),
                        ),
                      ],
                    ),
                  ),
                )
              : ListView.builder(
                  padding: const EdgeInsets.all(16),
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
                            SizedBox(
                              width: double.infinity,
                              child: FilledButton.icon(
                                onPressed: () => _trigger(p),
                                icon: const Icon(Icons.play_arrow, size: 18),
                                label: const Text('Run Pipeline'),
                              ),
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

/// Renders a pipeline as dependency layers.
///
/// Stages on the same row have no dependency between them and start together;
/// each row waits on the one above. Conditional edges are labelled, because
/// "runs only on success" changes what the graph means.
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
                            Text(n.archetypeId,
                                style: TextStyle(
                                    color: Fleet.ink400, fontSize: 10)),
                            if (conditions[n.id] != null)
                              Padding(
                                padding: const EdgeInsets.only(top: 3),
                                child: Text('only on ${conditions[n.id]}',
                                    style: TextStyle(
                                        color: Fleet.warn, fontSize: 9)),
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
              'Stages on a row run together; each row waits on the one above.',
              style: TextStyle(color: Fleet.ink400, fontSize: 10),
            ),
          ),
      ],
    );
  }
}
