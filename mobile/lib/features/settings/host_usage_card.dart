import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';

/// Live usage of the machine running the orchestrator.
///
/// Distinct from the per-agent meters on the fleet screen: those come from
/// Docker and describe one sandbox. This is the host itself, which is what
/// actually runs out — when the box is saturated every agent slows down at
/// once and no per-instance number explains why.
class HostUsageCard extends ConsumerWidget {
  const HostUsageCard({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final stats = ref.watch(hostStatsProvider);

    return Card(
      color: Fleet.ink850,
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.dns_outlined, size: 18),
                const SizedBox(width: 8),
                Text('Server', style: Theme.of(context).textTheme.titleSmall),
                const Spacer(),
                stats.when(
                  loading: () => const SizedBox(
                    width: 12,
                    height: 12,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                  error: (_, __) => Icon(Icons.cloud_off_outlined,
                      size: 16, color: Fleet.bad),
                  data: (s) => Text(
                    'up ${_uptime(s.uptimeSec)}',
                    style: TextStyle(color: Fleet.ink300, fontSize: 12),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 12),
            stats.when(
              loading: () => Text('Reading host usage...',
                  style: TextStyle(color: Fleet.ink300, fontSize: 12)),
              error: (e, _) => Text('Could not read host usage: $e',
                  style: TextStyle(color: Fleet.bad, fontSize: 12)),
              data: _Body.new,
            ),
            const Divider(height: 20),
            // The orchestrator's own figures, from /healthz — its instance
            // ceiling and event bus rather than the box it runs on.
            ref.watch(platformHealthProvider).when(
                  loading: () => Text('Reading platform health...',
                      style: TextStyle(color: Fleet.ink300, fontSize: 12)),
                  error: (e, _) => Text('Could not read /healthz: $e',
                      style: TextStyle(color: Fleet.bad, fontSize: 12)),
                  data: _PlatformRows.new,
                ),
          ],
        ),
      ),
    );
  }

  static String _uptime(double seconds) {
    final d = Duration(seconds: seconds.round());
    if (d.inDays > 0) return '${d.inDays}d ${d.inHours % 24}h';
    if (d.inHours > 0) return '${d.inHours}h ${d.inMinutes % 60}m';
    return '${d.inMinutes}m';
  }
}

class _Body extends StatelessWidget {
  const _Body(this.s);
  final HostStats s;

  @override
  Widget build(BuildContext context) {
    final gpu = s.gpu;
    return Column(
      children: [
        _Bar(
          label: 'CPU',
          fraction: (s.cpuPercent / 100).clamp(0, 1).toDouble(),
          detail: '${s.cpuPercent.toStringAsFixed(0)}%  ·  '
              '${s.cpuCores} cores  ·  load ${s.load1.toStringAsFixed(2)}',
        ),
        _Bar(
          label: 'Memory',
          fraction: s.memoryFraction,
          detail: '${_gb(s.memoryUsed)} / ${_gb(s.memoryTotal)}',
        ),
        _Bar(
          label: 'Disk',
          fraction: s.diskFraction,
          detail: '${_gb(s.diskUsed)} / ${_gb(s.diskTotal)}',
        ),
        if (gpu != null)
          _Bar(
            label: 'GPU',
            fraction: gpu.memoryFraction,
            detail: '${_gb(gpu.memoryUsed)} / ${_gb(gpu.memoryTotal)}'
                '  ·  ${gpu.utilisation.toStringAsFixed(0)}% util'
                '${gpu.temperatureC > 0 ? '  ·  ${gpu.temperatureC.toStringAsFixed(0)}°C' : ''}',
            sublabel: gpu.name,
          )
        else if (s.gpuMessage.isNotEmpty)
          // Absence is reported rather than shown as an empty bar: "no GPU"
          // and "the reading failed" are different things and the operator
          // should be able to tell them apart.
          Padding(
            padding: const EdgeInsets.only(top: 4),
            child: Row(
              children: [
                Icon(Icons.info_outline, size: 13, color: Fleet.ink300),
                const SizedBox(width: 6),
                Expanded(
                  child: Text(s.gpuMessage,
                      style: TextStyle(color: Fleet.ink300, fontSize: 11)),
                ),
              ],
            ),
          ),
      ],
    );
  }

  static String _gb(int bytes) {
    const gb = 1024 * 1024 * 1024;
    if (bytes >= gb) return '${(bytes / gb).toStringAsFixed(1)} GB';
    return '${(bytes / (1024 * 1024)).toStringAsFixed(0)} MB';
  }
}

/// Platform figures rendered as a compact grid of label/value pairs.
class _PlatformRows extends StatelessWidget {
  const _PlatformRows(this.h);
  final PlatformHealth h;

  @override
  Widget build(BuildContext context) {
    final entries = [
      ('Live instances', '${h.liveInstances} / ${h.maxInstances}'),
      ('Console clients', '${h.wsSubscribers}'),
      ('Events dropped', '${h.eventsDropped}'),
      ('Status', h.status),
    ];
    return Row(
      children: [
        for (final (label, value) in entries)
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(label.toUpperCase(),
                    style: TextStyle(
                        color: Fleet.ink400,
                        fontSize: 8.5,
                        fontWeight: FontWeight.w700,
                        letterSpacing: 0.4)),
                const SizedBox(height: 2),
                Text(value,
                    style: TextStyle(
                      fontFamily: 'monospace',
                      fontSize: 13,
                      // Dropped events mean some client rendered a stale
                      // picture; that deserves a colour, not just a number.
                      color: label == 'Events dropped' && h.eventsDropped > 0
                          ? Fleet.warn
                          : Fleet.ink100,
                    )),
              ],
            ),
          ),
      ],
    );
  }
}

class _Bar extends StatelessWidget {
  const _Bar({
    required this.label,
    required this.fraction,
    required this.detail,
    this.sublabel,
  });

  final String label;
  final double fraction;
  final String detail;
  final String? sublabel;

  @override
  Widget build(BuildContext context) {
    final f = fraction.isNaN ? 0.0 : fraction.clamp(0.0, 1.0);
    // Colour is a judgement, not decoration: a bar the operator should act on
    // has to look different from one that is merely busy.
    final colour = f > 0.9
        ? Fleet.bad
        : f > 0.75
            ? Fleet.warn
            : Fleet.good;

    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              SizedBox(
                width: 58,
                child: Text(label,
                    style: const TextStyle(
                        fontSize: 12, fontWeight: FontWeight.w600)),
              ),
              Expanded(
                child: Text(detail,
                    style: TextStyle(color: Fleet.ink300, fontSize: 11)),
              ),
            ],
          ),
          if (sublabel != null)
            Padding(
              padding: const EdgeInsets.only(left: 58, top: 1),
              child: Text(sublabel!,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(color: Fleet.ink300, fontSize: 10)),
            ),
          const SizedBox(height: 5),
          ClipRRect(
            borderRadius: BorderRadius.circular(3),
            child: LinearProgressIndicator(
              value: f,
              minHeight: 5,
              backgroundColor: Fleet.ink800,
              valueColor: AlwaysStoppedAnimation(colour),
            ),
          ),
        ],
      ),
    );
  }
}
