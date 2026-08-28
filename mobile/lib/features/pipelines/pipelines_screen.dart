import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';

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
        title: const Text('⛓️ Workflow DAG Pipelines'),
        actions: [
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
                            // Stages
                            Wrap(
                              spacing: 8,
                              runSpacing: 4,
                              children: p.nodes.map((n) {
                                return Chip(
                                  backgroundColor: Fleet.ink950,
                                  side: BorderSide(color: Fleet.ink800),
                                  label: Text(
                                    '${n.name} (${n.archetypeId})',
                                    style: const TextStyle(fontSize: 11),
                                  ),
                                );
                              }).toList(),
                            ),
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
