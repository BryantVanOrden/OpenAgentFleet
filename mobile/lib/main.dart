import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'core/network/api_client.dart';
import 'core/notifications/push.dart';
import 'core/state.dart';
import 'core/theme/theme.dart';
import 'core/theme/theme_controller.dart';
import 'features/alerts/alerts_screen.dart';
import 'features/auth/login_screen.dart';
import 'features/dashboard/fleet_screen.dart';
import 'features/settings/settings_screen.dart';
import 'features/swarms/swarms_screen.dart';
import 'features/voice/voice_screen.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();

  final api = await ApiClient.create();
  final prefs = await SharedPreferences.getInstance();

  // Registered before runApp so a notification arriving during a cold start is
  // not dropped.
  FirebaseMessaging.onBackgroundMessage(firebaseBackgroundHandler);

  runApp(
    ProviderScope(
      overrides: [
        apiProvider.overrideWithValue(api),
        sharedPreferencesProvider.overrideWithValue(prefs),
      ],
      child: AgentFleetApp(api: api),
    ),
  );
}

class AgentFleetApp extends ConsumerStatefulWidget {
  const AgentFleetApp({super.key, required this.api});
  final ApiClient api;

  @override
  ConsumerState<AgentFleetApp> createState() => _AgentFleetAppState();
}

class _AgentFleetAppState extends ConsumerState<AgentFleetApp> {
  final _navigator = GlobalKey<NavigatorState>();
  late final PushService _push = PushService(widget.api);

  @override
  void initState() {
    super.initState();
    _push.onAlertTapped = (_) {
      // Every alert lives in the resolution centre; deep-linking straight to one
      // and losing the rest of the queue is worse for triage.
      _navigator.currentState?.push(
        MaterialPageRoute(builder: (_) => const AlertsScreen()),
      );
    };
    if (widget.api.isAuthenticated) {
      _push.init();
    }
  }

  @override
  Widget build(BuildContext context) {
    final choice = ref.watch(themeControllerProvider);

    return MaterialApp(
      title: 'AgentFleet',
      navigatorKey: _navigator,
      debugShowCheckedModeBanner: false,
      themeMode: choice.mode,
      theme: buildTheme(Brightness.light, choice.accent),
      darkTheme: buildTheme(Brightness.dark, choice.accent),
      // Both palettes get built above; this pins the static accessors to
      // whichever one is actually on screen.
      builder: (context, child) =>
          FleetThemeSync(child: child ?? const SizedBox.shrink()),
      home: widget.api.isAuthenticated
          ? const HomeShell()
          : LoginScreen(onSignedIn: () => _push.init()),
    );
  }
}

/// Bottom-nav shell: fleet, alerts, settings. Three destinations because a phone
/// is for triage, not administration — the console does the rest.
class HomeShell extends ConsumerStatefulWidget {
  const HomeShell({super.key});

  @override
  ConsumerState<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends ConsumerState<HomeShell> {
  int _index = 0;

  @override
  Widget build(BuildContext context) {
    final blocking = ref.watch(blockingAlertCountProvider);
    final connected = ref.watch(connectionProvider).valueOrNull ?? false;

    return Scaffold(
      body: IndexedStack(
        index: _index,
        children: const [
          FleetScreen(),
          SwarmsScreen(),
          VoiceScreen(),
          AlertsScreen(),
          SettingsScreen(),
        ],
      ),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _index,
        onDestinationSelected: (i) => setState(() => _index = i),
        destinations: [
          const NavigationDestination(
            icon: Icon(Icons.grid_view_outlined),
            selectedIcon: Icon(Icons.grid_view_rounded),
            label: 'Fleet',
          ),
          const NavigationDestination(
            icon: Icon(Icons.hub_outlined),
            selectedIcon: Icon(Icons.hub_rounded),
            label: 'Swarms',
          ),
          const NavigationDestination(
            icon: Icon(Icons.mic_none_outlined),
            selectedIcon: Icon(Icons.mic_rounded),
            label: 'Voice',
          ),
          NavigationDestination(
            icon: Badge(
              isLabelVisible: blocking > 0,
              label: Text('$blocking'),
              child: const Icon(Icons.notifications_outlined),
            ),
            selectedIcon: const Icon(Icons.notifications_rounded),
            label: 'Alerts',
          ),
          NavigationDestination(
            icon: Icon(
              connected ? Icons.settings_outlined : Icons.cloud_off_outlined,
              color: connected ? null : Fleet.bad,
            ),
            selectedIcon: const Icon(Icons.settings_rounded),
            label: 'Settings',
          ),
        ],
      ),
    );
  }
}
