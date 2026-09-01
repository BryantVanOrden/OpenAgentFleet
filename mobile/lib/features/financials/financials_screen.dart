import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';

/// Token, cost and latency telemetry for the whole fleet: what the models are
/// costing and how fast they answer, with the most recent round-trips below.
class FinancialsScreen extends ConsumerStatefulWidget {
  const FinancialsScreen({super.key});

  @override
  ConsumerState<FinancialsScreen> createState() => _FinancialsScreenState();
}

class _FinancialsScreenState extends ConsumerState<FinancialsScreen> {
  FinancialSummary _summary = const FinancialSummary();
  List<TokenTelemetryRecord> _records = const [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    try {
      final api = ref.read(apiProvider);
      final results = await Future.wait([
        api.financialSummary(),
        api.telemetryRecords(limit: 50),
      ]);
      if (!mounted) return;
      setState(() {
        _summary = results[0] as FinancialSummary;
        _records = results[1] as List<TokenTelemetryRecord>;
        _loading = false;
        _error = null;
      });
    } catch (err) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = '$err';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Financials'),
        actions: [
          IconButton(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: _refresh,
          ),
        ],
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : _error != null
              ? Center(
                  child: Padding(
                    padding: const EdgeInsets.all(24),
                    child: Text('Could not load telemetry: $_error',
                        textAlign: TextAlign.center,
                        style: TextStyle(color: Fleet.bad, fontSize: 12)),
                  ),
                )
              : RefreshIndicator(
                  onRefresh: _refresh,
                  child: ListView(
                    padding: const EdgeInsets.fromLTRB(16, 12, 16, 24),
                    children: [
                      Row(
                        children: [
                          Expanded(
                            child: _StatCard(
                              label: 'Total spend',
                              value:
                                  '\$${_summary.totalCostUsd.toStringAsFixed(4)}',
                              hint: 'Fleet lifetime spend',
                              color: Fleet.live,
                            ),
                          ),
                          const SizedBox(width: 10),
                          Expanded(
                            child: _StatCard(
                              label: 'Prompt tokens',
                              value: _fmt(_summary.totalPromptTokens),
                              hint: 'Inbound context tokens',
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 10),
                      Row(
                        children: [
                          Expanded(
                            child: _StatCard(
                              label: 'Output tokens',
                              value: _fmt(_summary.totalCompletionTokens),
                              hint: 'Generated action tokens',
                            ),
                          ),
                          const SizedBox(width: 10),
                          Expanded(
                            child: _StatCard(
                              label: 'Avg latency',
                              value: '${_summary.avgLatencyMs} ms',
                              hint: 'Model round-trip',
                              color: Fleet.cool,
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 16),
                      Text('RECENT TURNS',
                          style: TextStyle(
                            color: Fleet.ink400,
                            fontSize: 10,
                            fontWeight: FontWeight.w700,
                            letterSpacing: 0.7,
                          )),
                      const SizedBox(height: 8),
                      if (_records.isEmpty)
                        Padding(
                          padding: const EdgeInsets.all(16),
                          child: Text('No telemetry recorded yet.',
                              style: TextStyle(
                                  color: Fleet.ink500,
                                  fontSize: 12,
                                  fontStyle: FontStyle.italic)),
                        )
                      else
                        Card(
                          child: Column(
                            children: [
                              for (var i = 0; i < _records.length; i++) ...[
                                if (i != 0) const Divider(height: 1),
                                _TurnRow(record: _records[i]),
                              ],
                            ],
                          ),
                        ),
                    ],
                  ),
                ),
    );
  }

  /// 1234567 -> 1,234,567. Done here rather than pulling in NumberFormat for
  /// one call site.
  static String _fmt(int n) {
    final s = '$n';
    final out = StringBuffer();
    for (var i = 0; i < s.length; i++) {
      if (i != 0 && (s.length - i) % 3 == 0) out.write(',');
      out.write(s[i]);
    }
    return out.toString();
  }
}

class _StatCard extends StatelessWidget {
  const _StatCard({
    required this.label,
    required this.value,
    required this.hint,
    this.color,
  });

  final String label;
  final String value;
  final String hint;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(label,
                style: TextStyle(
                    color: Fleet.ink400,
                    fontSize: 11,
                    fontFamily: 'monospace')),
            const SizedBox(height: 4),
            Text(
              value,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                color: color ?? Fleet.ink100,
                fontSize: 20,
                fontWeight: FontWeight.w700,
                fontFeatures: const [FontFeature.tabularFigures()],
              ),
            ),
            const SizedBox(height: 2),
            Text(hint, style: TextStyle(color: Fleet.ink500, fontSize: 10)),
          ],
        ),
      ),
    );
  }
}

class _TurnRow extends StatelessWidget {
  const _TurnRow({required this.record});
  final TokenTelemetryRecord record;

  @override
  Widget build(BuildContext context) {
    final mono = TextStyle(
        color: Fleet.ink400, fontSize: 10.5, fontFamily: 'monospace');

    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  record.modelName.isEmpty ? '(unknown model)' : record.modelName,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                      color: Fleet.live,
                      fontSize: 12,
                      fontFamily: 'monospace'),
                ),
              ),
              Text(humanAgo(record.createdAt),
                  style: TextStyle(color: Fleet.ink500, fontSize: 10.5)),
            ],
          ),
          const SizedBox(height: 4),
          Wrap(
            spacing: 12,
            runSpacing: 2,
            children: [
              Text('${record.promptTokens} in', style: mono),
              Text('${record.completionTokens} out', style: mono),
              Text('\$${record.costUsd.toStringAsFixed(6)}',
                  style: mono.copyWith(color: Fleet.good)),
              Text('${record.latencyMs} ms', style: mono),
            ],
          ),
        ],
      ),
    );
  }
}
