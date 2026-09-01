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
import 'features/admin/admin_screen.dart';
import 'features/auth/login_screen.dart';
import 'features/dashboard/fleet_screen.dart';
import 'features/pipelines/pipelines_screen.dart';
import 'features/settings/settings_screen.dart';
import 'features/splash/splash_screen.dart';
import 'features/vault/vault_screen.dart';

void main() {
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
          child: OpenAgentFleetApp(api: deps.api),
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

class OpenAgentFleetApp extends ConsumerStatefulWidget {
  const OpenAgentFleetApp({super.key, required this.api});
  final ApiClient api;

  @override
  ConsumerState<OpenAgentFleetApp> createState() => _OpenAgentFleetAppState();
}

class _OpenAgentFleetAppState extends ConsumerState<OpenAgentFleetApp> {
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
    _watchAlertsForNotifications();
  }

  /// Notify when an agent stops to ask something.
  ///
  /// Push needs a Firebase project this deployment does not have, so nothing
  /// ever reached the notification code and an agent waiting on a person waited
  /// silently. The event socket already carries the alert; this shows it. Only
  /// while the app is running -- real push is still the answer for a phone in a
  /// pocket -- but the common case here is an app that is open.
  void _watchAlertsForNotifications() {
    final seen = <String>{};
    ref.listenManual(alertsProvider, (previous, next) {
      final alerts = next.valueOrNull;
      if (alerts == null) return;
      final firstLoad = previous?.valueOrNull == null;
      for (final a in alerts) {
        if (a.resolvedAt != null || !seen.add(a.id)) continue;
        // The first load is the backlog, not news. Notifying for every alert
        // already sitting there would fire a dozen at once on launch.
        if (firstLoad) continue;
        _push.showAlert(
          id: a.id,
          title: a.title.isEmpty ? 'An agent needs you' : a.title,
          body: a.body,
          severity: a.severity,
        );
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final choice = ref.watch(themeControllerProvider);

    return MaterialApp(
      title: 'OpenAgentFleet',
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

  /// Tell the tab being opened to reload.
  ///
  /// Everything lives in an IndexedStack, so screens are built once and kept.
  /// Without this they show what they fetched when the app started.
  void _select(int i) {
    setState(() => _index = i);
    ref.read(tabRefreshProvider(i).notifier).state++;
  }

  @override
  Widget build(BuildContext context) {
    final blocking = ref.watch(blockingAlertCountProvider);
    final connected = ref.watch(connectionProvider).valueOrNull ?? false;
    // Administration is a separate place, and only for administrators. A
    // non-admin never sees the tab rather than being refused inside it.
    final isAdmin = ref.watch(meProvider).valueOrNull?.isAdmin ?? false;

    // Losing admin mid-session (signed out, role changed) must not leave the
    // stack pointing at a child that is no longer there.
    final pageCount = isAdmin ? 6 : 5;
    final index = _index < pageCount ? _index : 0;

    return Scaffold(
      body: IndexedStack(
        index: index,
        children: [
          const FleetScreen(),
          const PipelinesScreen(),
          const VaultScreen(),
          const AlertsScreen(),
          const SettingsScreen(),
          if (isAdmin) const AdminScreen(),
        ],
      ),
      bottomNavigationBar: NavigationBar(
        selectedIndex: index,
        onDestinationSelected: _select,
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
          if (isAdmin)
            const NavigationDestination(
              icon: Icon(Icons.admin_panel_settings_outlined),
              selectedIcon: Icon(Icons.admin_panel_settings),
              label: 'Admin',
            ),
        ],
      ),
    );
  }
}
