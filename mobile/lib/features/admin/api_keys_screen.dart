import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';

/// Long-lived keys for scripts and CI.
///
/// The secret exists once, in the response that creates it — there is no copy
/// to fetch later, which is why this screen makes a point of the moment it is
/// shown. Revoking keeps the row, so what a key did stays traceable after it
/// stops working.
class ApiKeysScreen extends ConsumerStatefulWidget {
  const ApiKeysScreen({super.key});

  @override
  ConsumerState<ApiKeysScreen> createState() => _ApiKeysScreenState();
}

class _ApiKeysScreenState extends ConsumerState<ApiKeysScreen> {
  List<ApiKey> _keys = const [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final keys = await ref.read(apiProvider).apiKeys();
      if (mounted) setState(() => _keys = keys);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('API keys'),
        actions: [
          IconButton(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: _load,
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _newKey,
        icon: const Icon(Icons.add),
        label: const Text('New key'),
      ),
      body: _loading && _keys.isEmpty
          ? const Center(child: CircularProgressIndicator())
          : RefreshIndicator(
              onRefresh: _load,
              child: ListView(
                padding: const EdgeInsets.fromLTRB(16, 12, 16, 96),
                children: [
                  if (_error != null) ...[
                    InlineError(_error!),
                    const SizedBox(height: 12),
                  ],
                  Text(
                    'A key acts with its owner\'s role. Revoking the key, or '
                    'disabling the person it belongs to, stops it immediately.',
                    style: TextStyle(
                        color: Fleet.ink400, fontSize: 12, height: 1.4),
                  ),
                  const SizedBox(height: 12),
                  for (final k in _keys) _tile(k),
                  if (_keys.isEmpty && !_loading)
                    Padding(
                      padding: const EdgeInsets.all(32),
                      child: Text(
                        'No keys issued.\n\nCreate one to let a script or a CI '
                        'job call this API without a password.',
                        textAlign: TextAlign.center,
                        style: TextStyle(color: Fleet.ink400, height: 1.4),
                      ),
                    ),
                ],
              ),
            ),
    );
  }

  String _when(DateTime? t) {
    if (t == null) return 'never used';
    final d = DateTime.now().difference(t);
    if (d.inMinutes < 1) return 'used just now';
    if (d.inHours < 1) return 'used ${d.inMinutes}m ago';
    if (d.inDays < 1) return 'used ${d.inHours}h ago';
    return 'used ${d.inDays}d ago';
  }

  Widget _tile(ApiKey k) => Card(
        color: Fleet.ink850,
        child: ListTile(
          leading: Icon(
            k.revoked ? Icons.key_off_outlined : Icons.vpn_key_outlined,
            color: k.revoked ? Fleet.bad : Fleet.ink300,
          ),
          title: Text(
            k.name,
            style: TextStyle(
                decoration: k.revoked ? TextDecoration.lineThrough : null),
          ),
          subtitle: Text(
            k.revoked
                ? 'revoked · ${k.userEmail}'
                : '${k.userEmail} · ${_when(k.lastUsedAt)}',
            style: TextStyle(color: Fleet.ink400, fontSize: 11),
          ),
          trailing: k.revoked
              ? null
              : IconButton(
                  tooltip: 'Revoke',
                  icon: Icon(Icons.block, color: Fleet.bad),
                  onPressed: () => _revoke(k),
                ),
        ),
      );

  Future<void> _revoke(ApiKey k) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        backgroundColor: Fleet.ink850,
        title: Text('Revoke "${k.name}"?'),
        content: Text(
          'Anything using this key stops working immediately. This cannot be '
          'undone — you would have to issue a new key.',
          style: TextStyle(color: Fleet.ink300),
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Fleet.bad),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Revoke'),
          ),
        ],
      ),
    );
    if (ok != true) return;
    try {
      await ref.read(apiProvider).revokeApiKey(k.id);
      await _load();
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    }
  }

  Future<void> _newKey() async {
    final name = TextEditingController();
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        backgroundColor: Fleet.ink850,
        title: const Text('New API key'),
        content: TextField(
          controller: name,
          autofocus: true,
          decoration: const InputDecoration(
            labelText: 'What is it for?',
            helperText: 'e.g. "nightly backup" — so it is clear what breaks '
                'if you revoke it',
          ),
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel')),
          FilledButton(
              onPressed: () => Navigator.pop(ctx, true),
              child: const Text('Create')),
        ],
      ),
    );
    if (ok != true || name.text.trim().isEmpty) return;

    try {
      final key = await ref.read(apiProvider).createApiKey(name.text.trim());
      await _load();
      if (mounted) await _showSecret(key);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    }
  }

  /// The one and only time the secret is visible.
  Future<void> _showSecret(ApiKey key) => showDialog<void>(
        context: context,
        barrierDismissible: false,
        builder: (ctx) => AlertDialog(
          backgroundColor: Fleet.ink850,
          title: const Text('Copy this now'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(
                'This is the only time this key is shown. It is not stored '
                'anywhere you can read it back — if you lose it, issue a new '
                'one and revoke this.',
                style: TextStyle(
                    color: Fleet.ink300, fontSize: 12, height: 1.4),
              ),
              const SizedBox(height: 12),
              Container(
                padding: const EdgeInsets.all(10),
                decoration: BoxDecoration(
                  color: Fleet.ink900,
                  borderRadius: BorderRadius.circular(8),
                ),
                child: SelectableText(
                  key.secret,
                  style: const TextStyle(
                      fontFamily: 'monospace', fontSize: 11, height: 1.4),
                ),
              ),
            ],
          ),
          actions: [
            TextButton.icon(
              onPressed: () {
                Clipboard.setData(ClipboardData(text: key.secret));
                ScaffoldMessenger.of(ctx).showSnackBar(
                  const SnackBar(content: Text('Key copied')),
                );
              },
              icon: const Icon(Icons.copy, size: 16),
              label: const Text('Copy'),
            ),
            FilledButton(
                onPressed: () => Navigator.pop(ctx),
                child: const Text('Done')),
          ],
        ),
      );
}
