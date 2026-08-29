import 'package:desktop_webview_window/desktop_webview_window.dart';
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
import 'features/pipelines/pipelines_screen.dart';
import 'features/settings/settings_screen.dart';
import 'features/splash/splash_screen.dart';
import 'features/vault/vault_screen.dart';

void main(List<String> args) {
  // desktop_webview_window re-executes this binary to host its title bar; that
  // process must render only the title bar and nothing else, so it returns
  // before the app is built.
  if (runWebViewTitleBarWidget(args)) return;

  WidgetsFlutterBinding.ensureInitialized();

  // Registered before runApp so a notification arriving during a cold start is
  // not dropped.
  FirebaseMessaging.onBackgroundMessage(firebaseBackgroundHandler);

  // runApp goes first and the disk work happens behind a splash. Awaiting it
  // out here instead would mean no window at all until it finished, which on
  // desktop -- where there is no native splash to cover the gap -- looks like
  // the app failed to launch.
  runApp(const Bootstrap());
}

/// Loads what the provider scope needs, showing the splash until it is ready.
class Bootstrap extends StatefulWidget {
  const Bootstrap({super.key});

  @override
  State<Bootstrap> createState() => _BootstrapState();
}

class _BootstrapState extends State<Bootstrap> {
  late Future<_Deps> _deps = _load();

  static Future<_Deps> _load() async {
    final api = await ApiClient.create();
    final prefs = await SharedPreferences.getInstance();
    return _Deps(api, prefs);
  }

  @override
  Widget build(BuildContext context) {
    return FutureBuilder<_Deps>(
      future: _deps,
      builder: (context, snap) {
        // The splash and error states get their own MaterialApp because the
        // themed one below cannot be built until the providers exist.
        if (snap.hasError) {
          return MaterialApp(
            debugShowCheckedModeBanner: false,
            home: SplashError(
              error: snap.error!,
              onRetry: () => setState(() => _deps = _load()),
            ),
          );
        }
        final deps = snap.data;
        if (deps == null) {
          return const MaterialApp(
            debugShowCheckedModeBanner: false,
            home: SplashScreen(),
          );
        }
        return ProviderScope(
          overrides: [
            apiProvider.overrideWithValue(deps.api),
            sharedPreferencesProvider.overrideWithValue(deps.prefs),
          ],
          child: AgentFleetApp(api: deps.api),
        );
      },
    );
  }
}

class _Deps {
  const _Deps(this.api, this.prefs);
  final ApiClient api;
  final SharedPreferences prefs;
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

/// Bottom-nav shell.
///
/// Five destinations, down from seven. Voice and Swarms were pulled out as
/// top-level tabs because neither is a place you go: voice is one of the ways
/// to talk to a particular agent, so it lives inside that agent alongside chat,
/// and agents now message each other automatically over the fleet bus, so a
/// swarm is not a thing you assemble and then visit. Their content did not
/// disappear; it moved to where it is used.
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
          PipelinesScreen(),
          VaultScreen(),
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
            icon: Icon(Icons.account_tree_outlined),
            selectedIcon: Icon(Icons.account_tree_rounded),
            label: 'Pipelines',
          ),
          const NavigationDestination(
            icon: Icon(Icons.lock_outline),
            selectedIcon: Icon(Icons.lock_rounded),
            label: 'Vault',
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
