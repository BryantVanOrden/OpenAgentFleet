import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../settings/providers_screen.dart';
import 'api_keys_screen.dart';
import 'orgs_screen.dart';
import 'users_screen.dart';

/// Deployment administration.
///
/// Its own tab rather than a corner of Settings: these are different jobs.
/// Settings is what one person prefers; this is what everyone else is allowed
/// to do, which engines the fleet runs on, and who holds a key to it. Only
/// administrators see the tab at all — the routes behind it refuse anyone
/// else regardless, but being refused inside a screen you were offered is a
/// worse way to learn that than never being offered it.
class AdminScreen extends ConsumerWidget {
  const AdminScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // Re-read the lists behind these screens when the tab is opened, so an
    // administrator who just changed something elsewhere is not looking at
    // what was true when the app started.
    ref.listen(tabRefreshProvider(Tabs.admin), (_, __) {
      ref.invalidate(aiProvidersProvider);
      ref.invalidate(meProvider);
    });

    return Scaffold(
      appBar: AppBar(
        title: const Text('Admin'),
        actions: [
          IconButton(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: () {
              ref.invalidate(aiProvidersProvider);
              ref.invalidate(instancesProvider);
              ref.invalidate(meProvider);
            },
          ),
        ],
      ),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
        children: [
          _section('People'),
          _card(
            context,
            icon: Icons.group_outlined,
            title: 'Users',
            subtitle: 'Create accounts, set passwords, change roles, revoke access',
            open: () => const UsersScreen(),
          ),
          _card(
            context,
            icon: Icons.apartment_outlined,
            title: 'Departments and access',
            subtitle: 'Which bots belong where, and who can see and drive them',
            open: () => const OrgsScreen(),
          ),
          const SizedBox(height: 20),
          _section('The fleet'),
          _card(
            context,
            icon: Icons.hub_outlined,
            title: 'AI connections',
            subtitle: 'Add engines, pick models, set the fallback order',
            open: () => const ProvidersScreen(),
          ),
          _card(
            context,
            icon: Icons.vpn_key_outlined,
            title: 'API keys',
            subtitle: 'Keys issued against this deployment, and revoking them',
            open: () => const ApiKeysScreen(),
          ),
        ],
      ),
    );
  }

  Widget _section(String label) => Padding(
        padding: const EdgeInsets.only(left: 4, bottom: 8),
        child: Text(
          label.toUpperCase(),
          style: TextStyle(
            color: Fleet.ink400,
            fontSize: 10,
            fontWeight: FontWeight.w700,
            letterSpacing: 0.7,
          ),
        ),
      );

  Widget _card(
    BuildContext context, {
    required IconData icon,
    required String title,
    required String subtitle,
    required Widget Function() open,
  }) =>
      Card(
        color: Fleet.ink850,
        child: ListTile(
          leading: Icon(icon, color: Fleet.ink300),
          title: Text(title),
          subtitle: Text(subtitle,
              style: TextStyle(color: Fleet.ink400, fontSize: 11)),
          trailing: const Icon(Icons.chevron_right),
          onTap: () => Navigator.of(context)
              .push(MaterialPageRoute(builder: (_) => open())),
        ),
      );
}
