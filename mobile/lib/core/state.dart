import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'models.dart';
import 'network/api_client.dart';
import 'network/events.dart';

/// Set once at startup in main().
final apiProvider = Provider<ApiClient>((_) => throw UnimplementedError('override in main'));

/// Bumped whenever the operator signs in or out, so dependent providers rebuild.
final sessionProvider = StateProvider<int>((_) => 0);

/// Fleet-wide event stream. Kept alive for the app's lifetime: it is what turns
/// this from a polling dashboard into something that reacts.
final fleetEventsProvider = Provider<EventStream>((ref) {
  ref.watch(sessionProvider);
  final stream = EventStream(ref.watch(apiProvider))..start();
  ref.onDispose(stream.dispose);
  return stream;
});

final connectionProvider = StreamProvider<bool>(
  (ref) => ref.watch(fleetEventsProvider).connected,
);

/// Instances, refreshed on any instance- or task-level event rather than on a
/// timer. A phone should not wake its radio every five seconds to poll.
final instancesProvider = StreamProvider<List<Instance>>((ref) async* {
  final api = ref.watch(apiProvider);
  ref.watch(sessionProvider);

  yield await api.instances();

  await for (final event in ref.watch(fleetEventsProvider).events) {
    if (event.type.startsWith('instance.') || event.type.startsWith('task.')) {
      yield await api.instances();
    }
  }
});

/// Live CPU/memory samples, keyed by instance.
final statsProvider = StreamProvider<Map<String, InstanceStats>>((ref) async* {
  final snapshot = <String, InstanceStats>{};
  await for (final event in ref.watch(fleetEventsProvider).events) {
    if (event.type != 'stats') continue;
    final payload = event.payload;
    if (payload is! Map) continue;
    final stats = InstanceStats.fromJson(payload.cast<String, dynamic>());
    snapshot[stats.instanceId] = stats;
    yield Map.of(snapshot);
  }
});

final alertsProvider = StreamProvider<List<Alert>>((ref) async* {
  final api = ref.watch(apiProvider);
  ref.watch(sessionProvider);

  yield await api.alerts();

  await for (final event in ref.watch(fleetEventsProvider).events) {
    if (event.type == 'alert' || event.type == 'alert.resolved') {
      yield await api.alerts();
    }
  }
});

/// Count of alerts actually blocking an agent — the badge on the nav bar.
final blockingAlertCountProvider = Provider<int>((ref) {
  final alerts = ref.watch(alertsProvider).valueOrNull ?? const [];
  return alerts.where((a) => a.isOpen).length;
});

final tasksProvider = StreamProvider.family<List<Task>, String>((ref, instanceId) async* {
  final api = ref.watch(apiProvider);
  yield await api.tasks(instanceId: instanceId);

  await for (final event in ref.watch(fleetEventsProvider).events) {
    if (event.instanceId == instanceId && event.type.startsWith('task.')) {
      yield await api.tasks(instanceId: instanceId);
    }
  }
});

final chatProvider = StreamProvider.family<List<ChatMessage>, String>((ref, instanceId) async* {
  final api = ref.watch(apiProvider);
  yield await api.chat(instanceId);

  await for (final event in ref.watch(fleetEventsProvider).events) {
    if (event.type == 'chat' && event.instanceId == instanceId) {
      yield await api.chat(instanceId);
    }
  }
});

final skillsProvider = FutureProvider<List<Skill>>(
  (ref) => ref.watch(apiProvider).skills(),
);
