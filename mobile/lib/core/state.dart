import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'models.dart';
import 'network/api_client.dart';
import 'network/events.dart';

/// Set once at startup in main().
final apiProvider =
    Provider<ApiClient>((_) => throw UnimplementedError('override in main'));

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

final tasksProvider =
    StreamProvider.family<List<Task>, String>((ref, instanceId) async* {
  final api = ref.watch(apiProvider);
  yield await api.tasks(instanceId: instanceId);

  await for (final event in ref.watch(fleetEventsProvider).events) {
    if (event.instanceId == instanceId && event.type.startsWith('task.')) {
      yield await api.tasks(instanceId: instanceId);
    }
  }
});

/// Which chat with which bot. A bot can hold several separate chats, and each
/// one is its own context, so the history has to be fetched per chat rather
/// than per bot.
typedef ChatRef = ({String instanceId, String chatId});

final chatProvider =
    StreamProvider.family<List<ChatMessage>, ChatRef>((ref, key) async* {
  final api = ref.watch(apiProvider);
  yield await api.chat(key.instanceId, chatId: key.chatId);

  await for (final event in ref.watch(fleetEventsProvider).events) {
    if (event.type == 'chat' && event.instanceId == key.instanceId) {
      yield await api.chat(key.instanceId, chatId: key.chatId);
    }
  }
});

final skillsProvider = FutureProvider<List<Skill>>(
  (ref) => ref.watch(apiProvider).skills(),
);

final aiProvidersProvider = FutureProvider<List<AIProvider>>(
  (ref) => ref.watch(apiProvider).providers(),
);

/// Sandbox tiers offered by the orchestrator. Fetched rather than hardcoded so
/// the picker cannot drift from what the server will actually accept — it
/// rejects an unknown tier outright rather than substituting a smaller one.
final tiersProvider = FutureProvider<List<TierProfile>>(
    (ref) => ref.watch(apiProvider).tiers());

/// Bot archetypes, as starting points for a new agent.
final templatesProvider = FutureProvider<List<BotTemplate>>(
    (ref) => ref.watch(apiProvider).templates());

/// Live usage of the machine running the orchestrator, polled while on screen.
/// Separate from [statsProvider], which is per sandbox.
final hostStatsProvider = StreamProvider<HostStats>((ref) async* {
  final api = ref.watch(apiProvider);
  while (true) {
    try {
      yield await api.hostStats();
    } catch (_) {
      // A failed sample is not worth tearing the stream down for; the next
      // tick usually succeeds, and the UI keeps showing the last good numbers.
    }
    await Future<void>.delayed(const Duration(seconds: 5));
  }
});

/// Scheduled wakeups and inbound hooks. Polled rather than streamed: schedules
/// change rarely, and a websocket topic for them would be more machinery than
/// the data justifies.
final cronTriggersProvider = FutureProvider<List<CronTrigger>>(
    (ref) => ref.watch(apiProvider).cronTriggers());

final webhookTriggersProvider = FutureProvider<List<WebhookTrigger>>(
    (ref) => ref.watch(apiProvider).webhookTriggers());

/// Voices the server's speech service offers. Empty when no TTS service is
/// deployed, which is a supported configuration rather than an error.
final serverVoicesProvider = FutureProvider<List<ServerVoice>>(
    (ref) => ref.watch(apiProvider).serverVoices());
