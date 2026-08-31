import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import 'provider_edit_sheet.dart';
import 'model_combo_sheet.dart';
import 'provider_signin_sheet.dart';

/// The AI engines the fleet can think with.
///
/// Order is the fallback order: the first connection that answers is used, and
/// the rest are there for when it does not. Drag to reorder, because which
/// model runs first is the setting most worth changing and the hardest to
/// express any other way.
class ProvidersScreen extends ConsumerStatefulWidget {
  const ProvidersScreen({super.key});

  @override
  ConsumerState<ProvidersScreen> createState() => _ProvidersScreenState();
}

/// Engines that can be signed into with an account.
const _canSignIn = {'gemini', 'antigravity'};

class _ProvidersScreenState extends ConsumerState<ProvidersScreen> {
  List<AIProvider> _providers = const [];
  List<ModelCombo> _combos = const [];
  bool _loading = true;
  String? _error;
  String _probing = '';

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    try {
      final api = ref.read(apiProvider);
      final list = await api.providers();
      final combos = await api.modelCombos();
      if (!mounted) return;
      setState(() {
        _providers = list;
        _combos = combos;
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

  Future<void> _edit([AIProvider? existing]) async {
    final result = await ProviderEditSheet.show(context, existing: existing);
    if (!mounted) return;
    await _refresh();
    // The sheet hands back the saved connection when the operator asked to
    // sign in to it, so the sign-in opens here rather than stacked on top of
    // the sheet that started it.
    if (result is AIProvider) await _signIn(result);
  }

  Future<void> _reorder(int oldIndex, int newIndex) async {
    // ReorderableListView reports newIndex against the list as it was BEFORE
    // the dragged item was taken out, so dragging downwards lands one place
    // short without this. The previous comment here claimed the callback had
    // already accounted for it, which is not how the widget behaves.
    if (newIndex > oldIndex) newIndex -= 1;

    final next = [..._providers];
    next.insert(newIndex, next.removeAt(oldIndex));
    // Optimistic: the list must not snap back under the finger while the
    // request is in flight.
    setState(() => _providers = next);

    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref
          .read(apiProvider)
          .reorderProviders(next.map((p) => p.id).toList());
      await _refresh();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
      await _refresh();
    }
  }

  Future<void> _signIn(AIProvider p) async {
    final ok = await ProviderSignInSheet.show(context, p);
    if (!mounted) return;
    if (ok == true) {
      ScaffoldMessenger.of(context)
          .showSnackBar(SnackBar(content: Text('Signed in to ${p.name}')));
    }
    await _refresh();
  }

  Future<void> _signOut(AIProvider p) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Sign out of ${p.name}?'),
        content: const Text(
          'The stored sign-in is deleted from the server vault and the '
          'connection goes back to using an API key. Bots pointed at it fall '
          'through to the next connection until you sign in again.',
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Fleet.bad),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Sign out'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;

    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(apiProvider).providerSignOut(p.id);
      await _refresh();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  Future<void> _probe(AIProvider p) async {
    setState(() => _probing = p.id);
    final messenger = ScaffoldMessenger.of(context);
    try {
      final res = await ref.read(apiProvider).probeProvider(p.id);
      final ok = res['ok'] == true || res['healthy'] == true;
      final detail = '${res['error'] ?? res['message'] ?? res['model'] ?? ''}';
      messenger.showSnackBar(SnackBar(
        content: Text(ok
            ? '${p.name} answered${detail.isEmpty ? '' : ' · $detail'}'
            : '${p.name} did not answer${detail.isEmpty ? '' : ': $detail'}'),
      ));
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    } finally {
      if (mounted) setState(() => _probing = '');
    }
  }

  Future<void> _delete(AIProvider p) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Remove ${p.name}?'),
        content: const Text(
          'Bots pointed at this connection fall back to the next one that '
          'works, so nothing stops thinking — but any bot that named it '
          'specifically loses that preference.',
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Fleet.bad),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Remove'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;

    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(apiProvider).deleteProvider(p.id);
      await _refresh();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('AI connections'),
        bottom: PreferredSize(
          preferredSize: const Size.fromHeight(28),
          child: Padding(
            padding: const EdgeInsets.only(left: 16, bottom: 8, right: 16),
            child: Align(
              alignment: Alignment.centerLeft,
              child: Text(
                'Tried top to bottom. Drag to change which model runs first.',
                style: TextStyle(color: Fleet.ink400, fontSize: 11),
              ),
            ),
          ),
        ),
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => _edit(),
        icon: const Icon(Icons.add),
        label: const Text('Add'),
      ),
      body: _buildBody(),
    );
  }

  Widget _buildBody() {
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_error != null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Text('Could not load connections: $_error',
              textAlign: TextAlign.center,
              style: TextStyle(color: Fleet.bad, fontSize: 12)),
        ),
      );
    }
    if (_providers.isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Text(
            'No AI connections yet.\n\nAdd one and your fleet has something to '
            'think with.',
            textAlign: TextAlign.center,
            style: TextStyle(color: Fleet.ink400, fontSize: 13, height: 1.4),
          ),
        ),
      );
    }

    return Column(
      children: [
        Expanded(
          child: ReorderableListView.builder(
            padding: const EdgeInsets.fromLTRB(12, 12, 12, 12),
            itemCount: _providers.length,
            onReorder: _reorder,
            itemBuilder: (_, i) => _tile(_providers[i], i),
          ),
        ),
        _combosSection(),
      ],
    );
  }

  /// Combinations: which model does what. Shown beside connections because a
  /// combination is selectable anywhere a single connection is.
  Widget _combosSection() {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(12, 10, 12, 84),
      decoration: BoxDecoration(
        color: Fleet.ink900,
        border: Border(top: BorderSide(color: Fleet.ink800)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text('COMBINATIONS',
                    style: TextStyle(
                        color: Fleet.ink400,
                        fontSize: 10,
                        letterSpacing: 0.6,
                        fontWeight: FontWeight.w700)),
              ),
              TextButton.icon(
                onPressed: () async {
                  if (await ModelComboSheet.show(context) == true) {
                    await _refresh();
                  }
                },
                icon: const Icon(Icons.add, size: 15),
                label: const Text('New', style: TextStyle(fontSize: 11.5)),
              ),
            ],
          ),
          if (_combos.isEmpty)
            Text(
              'Pair a model that sees with one that reasons, then use the pair '
              "in a bot's model list.",
              style: TextStyle(color: Fleet.ink500, fontSize: 11, height: 1.4),
            )
          else
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 150),
              child: ListView(
                shrinkWrap: true,
                children: [for (final c in _combos) _comboTile(c)],
              ),
            ),
        ],
      ),
    );
  }

  Widget _comboTile(ModelCombo c) {
    String nameOf(String id) => _providers
        .where((p) => p.id == id)
        .map((p) => p.model)
        .firstOrNull ?? '—';

    final summary = c.roles.entries
        .map((e) => '${ModelCombo.roleShort[e.key] ?? e.key}: ${nameOf(e.value)}')
        .join('  ·  ');

    return Card(
      color: Fleet.ink850,
      margin: const EdgeInsets.only(bottom: 6),
      child: ListTile(
        dense: true,
        leading: Icon(c.isSimple ? Icons.psychology_alt_outlined : Icons.hub,
            size: 18, color: Fleet.cool),
        title: Text(c.name, style: const TextStyle(fontSize: 13)),
        subtitle: Text(summary,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(color: Fleet.ink400, fontSize: 10.5)),
        onTap: () async {
          if (await ModelComboSheet.show(context, existing: c) == true) {
            await _refresh();
          }
        },
        trailing: IconButton(
          icon: Icon(Icons.delete_outline, size: 18, color: Fleet.ink400),
          onPressed: () async {
            final messenger = ScaffoldMessenger.of(context);
            try {
              await ref.read(apiProvider).deleteModelCombo(c.id);
              await _refresh();
            } catch (err) {
              messenger.showSnackBar(SnackBar(content: Text('$err')));
            }
          },
        ),
      ),
    );
  }

  Widget _tile(AIProvider p, int index) {
    final first = index == 0 && p.enabled;

    return Card(
      key: ValueKey(p.id),
      color: Fleet.ink850,
      margin: const EdgeInsets.only(bottom: 8),
      child: ListTile(
        onTap: () => _edit(p),
        leading: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(Icons.drag_indicator, size: 18, color: Fleet.ink600),
          ],
        ),
        title: Row(
          children: [
            Flexible(
              child: Text(p.name.isEmpty ? p.kind : p.name,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                      fontSize: 14, fontWeight: FontWeight.w600)),
            ),
            const SizedBox(width: 6),
            if (first) _chip('FIRST', Fleet.good),
            if (!p.enabled) _chip('OFF', Fleet.ink500),
            if (!p.vision) _chip('NO VISION', Fleet.warn),
            // A connection set to sign in but never signed into looks
            // configured and fails every request; say so on the row.
            if (p.usesOAuth && !p.signedIn) _chip('SIGN IN', Fleet.warn),
            if (p.usesOAuth && p.signedIn) _chip('ACCOUNT', Fleet.cool),
          ],
        ),
        subtitle: Text(
          '${p.kind} · ${p.model.isEmpty ? 'no model set' : p.model}',
          style: TextStyle(color: Fleet.ink400, fontSize: 11),
        ),
        trailing: PopupMenuButton<String>(
          color: Fleet.ink850,
          icon: _probing == p.id
              ? const SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(strokeWidth: 2))
              : Icon(Icons.more_vert, size: 19, color: Fleet.ink400),
          onSelected: (a) => switch (a) {
            'edit' => _edit(p),
            'probe' => _probe(p),
            'signin' => _signIn(p),
            'signout' => _signOut(p),
            _ => _delete(p),
          },
          itemBuilder: (_) => [
            // Offered by engine rather than by stored auth mode: a connection
            // saved with a key can still be signed into, and hiding the option
            // until it was already OAuth meant it never appeared at all.
            if (_canSignIn.contains(p.kind))
              PopupMenuItem(
                value: p.signedIn ? 'signout' : 'signin',
                child: ListTile(
                  dense: true,
                  leading: Icon(p.signedIn ? Icons.logout : Icons.login),
                  title: Text(p.signedIn ? 'Sign out' : 'Sign in with Google'),
                ),
              ),
            const PopupMenuItem(
              value: 'edit',
              child: ListTile(
                  dense: true,
                  leading: Icon(Icons.tune),
                  title: Text('Configure')),
            ),
            const PopupMenuItem(
              value: 'probe',
              child: ListTile(
                  dense: true,
                  leading: Icon(Icons.wifi_tethering),
                  title: Text('Test connection')),
            ),
            PopupMenuItem(
              value: 'delete',
              child: ListTile(
                dense: true,
                leading: Icon(Icons.delete_outline, color: Fleet.bad),
                title: Text('Remove', style: TextStyle(color: Fleet.bad)),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _chip(String label, Color colour) => Container(
        margin: const EdgeInsets.only(right: 4),
        padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 1),
        decoration: BoxDecoration(
          color: colour.withValues(alpha: 0.16),
          borderRadius: BorderRadius.circular(4),
        ),
        child: Text(label,
            style: TextStyle(
                fontSize: 8.5, fontWeight: FontWeight.w700, color: colour)),
      );
}
