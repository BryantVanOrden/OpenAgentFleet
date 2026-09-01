import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';

/// The platform credential vault: sealed values tasks reference by name.
///
/// Values are encrypted with AES-256-GCM under MASTER_KEY and are never
/// readable through the API — a secret referenced by a task is written into
/// the sandbox keyring on a tmpfs for the life of the run, so a build script
/// can read it without it ever reaching a prompt or a log.
class CredentialsScreen extends ConsumerStatefulWidget {
  const CredentialsScreen({super.key});

  @override
  ConsumerState<CredentialsScreen> createState() => _CredentialsScreenState();
}

class _CredentialsScreenState extends ConsumerState<CredentialsScreen> {
  List<SecretRef> _refs = const [];
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
      final refs = await ref.read(apiProvider).secretRefs();
      if (mounted) setState(() => _refs = refs);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _delete(SecretRef s) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Delete ${s.ref}?'),
        content: Text(
          'Anything referencing it will stop working.',
          style: TextStyle(color: Fleet.ink300),
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Fleet.bad),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (ok != true) return;
    try {
      await ref.read(apiProvider).deleteSecretRef(s.ref);
      await _load();
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Credentials'),
        actions: [
          IconButton(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: _load,
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () async {
          final stored = await _StoreSheet.show(context);
          if (stored == true) _load();
        },
        icon: const Icon(Icons.add),
        label: const Text('Store'),
      ),
      body: _loading && _refs.isEmpty
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
                    'Values are sealed with AES-256-GCM under MASTER_KEY and '
                    'are never readable through the API. A secret referenced '
                    'by a task is written into the sandbox keyring on a tmpfs '
                    'for the life of the run.',
                    style: TextStyle(
                        color: Fleet.ink400, fontSize: 12, height: 1.4),
                  ),
                  const SizedBox(height: 12),
                  if (_refs.isEmpty && !_loading)
                    Padding(
                      padding: const EdgeInsets.all(32),
                      child: Text(
                        'Nothing stored yet.\n\nStore a token here and a task '
                        'can use it without the value ever reaching a prompt '
                        'or a log.',
                        textAlign: TextAlign.center,
                        style: TextStyle(color: Fleet.ink400, height: 1.4),
                      ),
                    ),
                  for (final s in _refs)
                    Card(
                      color: Fleet.ink850,
                      margin: const EdgeInsets.only(bottom: 8),
                      child: ListTile(
                        leading: Icon(Icons.lock_outline, color: Fleet.ink300),
                        title: Text(s.ref,
                            style: const TextStyle(
                                fontFamily: 'monospace', fontSize: 13)),
                        subtitle: Text(
                          '${s.note.isEmpty ? 'no note' : s.note}'
                          '${s.updatedAt != null ? ' · updated ${humanAgo(s.updatedAt!)}' : ''}',
                          style:
                              TextStyle(color: Fleet.ink400, fontSize: 11),
                        ),
                        trailing: IconButton(
                          tooltip: 'Delete',
                          icon: Icon(Icons.delete_outline, color: Fleet.bad),
                          onPressed: () => _delete(s),
                        ),
                      ),
                    ),
                ],
              ),
            ),
    );
  }
}

/// Store or replace one credential. Storing to an existing reference
/// overwrites the sealed value — that is how a token gets rotated.
class _StoreSheet extends ConsumerStatefulWidget {
  const _StoreSheet();

  static Future<bool?> show(BuildContext context) => showModalBottomSheet<bool>(
        context: context,
        isScrollControlled: true,
        backgroundColor: Fleet.ink900,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
        ),
        builder: (_) => const _StoreSheet(),
      );

  @override
  ConsumerState<_StoreSheet> createState() => _StoreSheetState();
}

class _StoreSheetState extends ConsumerState<_StoreSheet> {
  final _ref = TextEditingController();
  final _value = TextEditingController();
  final _note = TextEditingController();
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _ref.dispose();
    _value.dispose();
    _note.dispose();
    super.dispose();
  }

  Future<void> _store() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref
          .read(apiProvider)
          .putSecretRef(_ref.text.trim(), _value.text, _note.text.trim());
      if (mounted) Navigator.pop(context, true);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final inset = MediaQuery.of(context).viewInsets.bottom;
    final complete =
        _ref.text.trim().isNotEmpty && _value.text.isNotEmpty;

    return Padding(
      padding: EdgeInsets.fromLTRB(20, 18, 20, 18 + inset),
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                const Icon(Icons.lock_outline, size: 20),
                const SizedBox(width: 8),
                Text('Store a credential',
                    style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 16),
            TextField(
              controller: _ref,
              autofocus: true,
              autocorrect: false,
              style: const TextStyle(fontFamily: 'monospace', fontSize: 13),
              decoration: const InputDecoration(
                labelText: 'Reference',
                hintText: 'github/deploy_token',
                helperText: 'How tasks name it. Reusing one rotates the value.',
              ),
              onChanged: (_) => setState(() {}),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _value,
              obscureText: true,
              autocorrect: false,
              enableSuggestions: false,
              decoration: const InputDecoration(
                labelText: 'Value',
                helperText: 'Sealed on arrival; never readable back.',
              ),
              onChanged: (_) => setState(() {}),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _note,
              decoration: const InputDecoration(
                labelText: 'Note',
                hintText: 'what it opens, who rotated it last',
              ),
            ),
            InlineError(_error),
            const SizedBox(height: 14),
            FilledButton(
              onPressed: _busy || !complete ? null : _store,
              child: Text(_busy ? 'Storing...' : 'Store'),
            ),
          ],
        ),
      ),
    );
  }
}
