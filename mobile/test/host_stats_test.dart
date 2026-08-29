import 'package:agentfleet_companion/core/models.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('HostStats', () {
    test('parses a full payload including GPU', () {
      final s = HostStats.fromJson({
        'cpu_percent': 12.5,
        'cpu_cores': 16,
        'load1': 1.25,
        'memory_used_bytes': 8 * 1024 * 1024 * 1024,
        'memory_total_bytes': 32 * 1024 * 1024 * 1024,
        'disk_used_bytes': 100,
        'disk_total_bytes': 400,
        'uptime_sec': 3600.0,
        'gpu': {
          'name': 'NVIDIA GeForce RTX 3060 Laptop GPU',
          'memory_used_bytes': 3 * 1024 * 1024 * 1024,
          'memory_total_bytes': 6 * 1024 * 1024 * 1024,
          'utilisation_percent': 42.0,
          'temperature_c': 57.0,
        },
      });

      expect(s.cpuCores, 16);
      expect(s.memoryFraction, closeTo(0.25, 0.001));
      expect(s.diskFraction, closeTo(0.25, 0.001));
      expect(s.gpu, isNotNull);
      expect(s.gpu!.memoryFraction, closeTo(0.5, 0.001));
      expect(s.gpu!.temperatureC, 57.0);
    });

    // A host with no GPU is normal, and must be distinguishable from a broken
    // reading — hence the message rather than a silent null.
    test('absent GPU carries an explanation', () {
      final s = HostStats.fromJson({
        'cpu_percent': 1.0,
        'gpu_message': 'no GPU reading: nvidia-smi unavailable',
      });
      expect(s.gpu, isNull);
      expect(s.gpuMessage, contains('nvidia-smi'));
    });

    // Division by a zero total would render as NaN in the bars.
    test('fractions are zero rather than NaN when totals are missing', () {
      final s = HostStats.fromJson({});
      expect(s.memoryFraction, 0);
      expect(s.diskFraction, 0);
      expect(s.gpu, isNull);
    });
  });

  group('BotTemplate', () {
    test('tolerates a sparse payload', () {
      final t = BotTemplate.fromJson({'id': 'fleet_manager'});
      expect(t.id, 'fleet_manager');
      expect(t.name, '');
      expect(t.recommendedTier, '');
    });
  });
}
