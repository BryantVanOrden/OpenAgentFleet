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
  List<ModelCombo> _combos = const [];
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
      final api = ref.read(apiProvider);
      final list = await api.providers();
      final combos = await api.modelCombos();
      if (!mounted) return;
      setState(() {
        _all = list;
        _combos = combos;
        // Drop references to anything that no longer exists, so the sheet never
        // shows a slot for something you cannot see or reorder. A chain entry
        // is either a connection or a combination.
        _chain = _chain
            .where((id) =>
                list.any((p) => p.id == id) || combos.any((c) => c.id == id))
            .toList();
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
  ModelCombo? _comboById(String id) =>
      _combos.where((c) => c.id == id).firstOrNull;

  /// What to show for a chain entry, whichever kind it is.
  ({String title, String subtitle, bool isCombo}) _describe(String id) {
    final combo = _comboById(id);
    if (combo != null) {
      String modelOf(String pid) =>
          _all.where((p) => p.id == pid).map((p) => p.model).firstOrNull ?? '—';
      return (
        title: combo.name,
        subtitle: combo.roles.entries
            .map((e) =>
                '${ModelCombo.roleShort[e.key] ?? e.key}: ${modelOf(e.value)}')
            .join(' · '),
        isCombo: true,
      );
    }
    final p = _byId(id);
    return (
      title: p?.name ?? id,
      subtitle: p?.model ?? '',
      isCombo: false,
    );
  }

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
    if (_all.isEmpty && _combos.isEmpty) {
      return Text(
        'No AI connections configured. Add one in Settings first.',
        style: TextStyle(color: Fleet.ink400, fontSize: 12.5),
      );
    }

    final unchosenProviders =
        _all.where((p) => !_chain.contains(p.id)).toList();
    final unchosenCombos =
        _combos.where((c) => !_chain.contains(c.id)).toList();

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
              // ReorderableListView reports the new index against the list as
              // it was before the dragged item was removed, so dragging
              // downwards lands one place short without the adjustment. This
              // is the bot's model fallback order, where one place short means
              // a different model answers.
              onReorder: (oldIndex, newIndex) => setState(() {
                if (newIndex > oldIndex) newIndex -= 1;
                _chain.insert(newIndex, _chain.removeAt(oldIndex));
              }),
              itemBuilder: (_, i) {
                final d = _describe(_chain[i]);
                return Card(
                  key: ValueKey(_chain[i]),
                  color: Fleet.ink850,
                  margin: const EdgeInsets.only(bottom: 6),
                  child: ListTile(
                    dense: true,
                    leading: Icon(
                        d.isCombo ? Icons.hub : Icons.drag_indicator,
                        size: 17,
                        color: d.isCombo ? Fleet.cool : Fleet.ink600),
                    title: Text(d.title, style: const TextStyle(fontSize: 13)),
                    subtitle: Text(
                      '${i == 0 ? 'First choice' : 'Fallback $i'} · ${d.subtitle}',
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
          if (unchosenCombos.isNotEmpty) ...[
            _label('COMBINATIONS'),
            for (final c in unchosenCombos)
              Card(
                color: Fleet.ink900,
                margin: const EdgeInsets.only(bottom: 6),
                child: ListTile(
                  dense: true,
                  leading: Icon(Icons.hub, size: 17, color: Fleet.cool),
                  title: Text(c.name, style: const TextStyle(fontSize: 13)),
                  subtitle: Text(_describe(c.id).subtitle,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(color: Fleet.ink400, fontSize: 10.5)),
                  onTap: () => setState(() => _chain.add(c.id)),
                ),
              ),
            const SizedBox(height: 8),
          ],
          if (unchosenProviders.isNotEmpty) ...[
            _label(_chain.isEmpty ? 'SINGLE MODELS' : 'ADD AS FALLBACK'),
            for (final p in unchosenProviders)
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
