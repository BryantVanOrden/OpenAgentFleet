import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';

/// Which models this bot thinks with, and in what order.
///
/// The order is the fallback order: the first one that answers is used. A bot
/// doing form entry and a bot reading dense screenshots want different models,
/// and one fleet-wide order cannot express that.
class ModelChainSheet extends ConsumerStatefulWidget {
  const ModelChainSheet({super.key, required this.instance});

  final Instance instance;

  static Future<bool?> show(BuildContext context, Instance instance) =>
      showModalBottomSheet<bool>(
        context: context,
        isScrollControlled: true,
        builder: (_) => ModelChainSheet(instance: instance),
      );

  @override
  ConsumerState<ModelChainSheet> createState() => _ModelChainSheetState();
}

class _ModelChainSheetState extends ConsumerState<ModelChainSheet> {
  /// Errors show inline. This is a modal bottom sheet, and a snackbar raised
  /// from inside one renders behind the sheet — invisible, which makes a
  /// failed action look like a control that did nothing.

  List<AIProvider> _all = const [];
  late List<String> _chain = [...widget.instance.providerIds];
  bool _loading = true;
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final list = await ref.read(apiProvider).providers();
      if (!mounted) return;
      setState(() {
        _all = list;
        // Drop references to connections that no longer exist, so the sheet
        // never shows a slot for something you cannot see or reorder.
        _chain = _chain.where((id) => list.any((p) => p.id == id)).toList();
        _loading = false;
      });
    } catch (err) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = '$err';
      });
    }
  }

  AIProvider? _byId(String id) => _all.where((p) => p.id == id).firstOrNull;

  Future<void> _save() async {
    setState(() => _busy = true);
    final navigator = Navigator.of(context);
    try {
      await ref
          .read(apiProvider)
          .setInstanceModels(widget.instance.id, _chain);
      ref.invalidate(instancesProvider);
      navigator.pop(true);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: EdgeInsets.only(
        left: 16,
        right: 16,
        top: 16,
        bottom: MediaQuery.of(context).viewInsets.bottom + 16,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('Models for ${widget.instance.name}',
              style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w700)),
          const SizedBox(height: 4),
          Text(
            _chain.isEmpty
                ? 'Using the fleet order. Pick models to give this bot its own.'
                : 'Tried top to bottom. Drag to reorder.',
            style: TextStyle(color: Fleet.ink400, fontSize: 11),
          ),
          const SizedBox(height: 14),
          Flexible(child: _buildBody()),
          InlineError(_error),
          const SizedBox(height: 12),
          Row(
            children: [
              if (_chain.isNotEmpty)
                TextButton(
                  onPressed: _busy ? null : () => setState(_chain.clear),
                  child: const Text('Use fleet order'),
                ),
              const Spacer(),
              FilledButton(
                onPressed: _busy ? null : _save,
                child: _busy
                    ? const SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(strokeWidth: 2))
                    : const Text('Save'),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildBody() {
    if (_loading) {
      return const Padding(
        padding: EdgeInsets.all(28),
        child: Center(child: CircularProgressIndicator()),
      );
    }
    if (_error != null) {
      return Text('Could not load connections: $_error',
          style: TextStyle(color: Fleet.bad, fontSize: 12));
    }
    if (_all.isEmpty) {
      return Text(
        'No AI connections configured. Add one in Settings first.',
        style: TextStyle(color: Fleet.ink400, fontSize: 12.5),
      );
    }

    final unchosen = _all.where((p) => !_chain.contains(p.id)).toList();

    return SingleChildScrollView(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (_chain.isNotEmpty) ...[
            _label('THIS BOT USES'),
            ReorderableListView.builder(
              shrinkWrap: true,
              physics: const NeverScrollableScrollPhysics(),
              itemCount: _chain.length,
              onReorderItem: (o, n) => setState(() {
                _chain.insert(n, _chain.removeAt(o));
              }),
              itemBuilder: (_, i) {
                final p = _byId(_chain[i]);
                return Card(
                  key: ValueKey(_chain[i]),
                  color: Fleet.ink850,
                  margin: const EdgeInsets.only(bottom: 6),
                  child: ListTile(
                    dense: true,
                    leading: Icon(Icons.drag_indicator,
                        size: 17, color: Fleet.ink600),
                    title: Text(p?.name ?? _chain[i],
                        style: const TextStyle(fontSize: 13)),
                    subtitle: Text(
                      i == 0 ? 'First choice · ${p?.model ?? ''}'
                             : 'Fallback $i · ${p?.model ?? ''}',
                      style: TextStyle(color: Fleet.ink400, fontSize: 10.5),
                    ),
                    trailing: IconButton(
                      icon: Icon(Icons.remove_circle_outline,
                          size: 18, color: Fleet.ink400),
                      onPressed: () =>
                          setState(() => _chain.removeAt(i)),
                    ),
                  ),
                );
              },
            ),
            const SizedBox(height: 8),
          ],
          if (unchosen.isNotEmpty) ...[
            _label(_chain.isEmpty ? 'AVAILABLE' : 'ADD AS FALLBACK'),
            for (final p in unchosen)
              Card(
                color: Fleet.ink900,
                margin: const EdgeInsets.only(bottom: 6),
                child: ListTile(
                  dense: true,
                  leading: Icon(Icons.add_circle_outline,
                      size: 17, color: Fleet.ink400),
                  title: Text(p.name, style: const TextStyle(fontSize: 13)),
                  subtitle: Text(
                    '${p.kind} · ${p.model}'
                    '${p.vision ? '' : ' · no vision'}',
                    style: TextStyle(color: Fleet.ink400, fontSize: 10.5),
                  ),
                  onTap: () => setState(() => _chain.add(p.id)),
                ),
              ),
          ],
        ],
      ),
    );
  }

  Widget _label(String text) => Padding(
        padding: const EdgeInsets.only(bottom: 6),
        child: Text(text,
            style: TextStyle(
                color: Fleet.ink400,
                fontSize: 10,
                letterSpacing: 0.6,
                fontWeight: FontWeight.w700)),
      );
}
