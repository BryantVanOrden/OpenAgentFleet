import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';

/// What one bot has chosen to remember.
///
/// Each agent keeps its own memory, so what is here is what this bot decided
/// was worth carrying forward — not the fleet's shared notes. Anything wrong
/// can be removed: a bad conclusion recorded once is otherwise recalled every
/// time the agent looks something up.
class BotMemoryScreen extends ConsumerStatefulWidget {
  const BotMemoryScreen({
    super.key,
    required this.instanceId,
    required this.instanceName,
  });

  final String instanceId;
  final String instanceName;

  @override
  ConsumerState<BotMemoryScreen> createState() => _BotMemoryScreenState();
}

class _BotMemoryScreenState extends ConsumerState<BotMemoryScreen> {
  List<BotMemory> _memories = const [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    try {
      final list =
          await ref.read(apiProvider).instanceMemories(widget.instanceId);
      if (!mounted) return;
      setState(() {
        _memories = list;
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

  Future<void> _forget(BotMemory m) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Forget this?'),
        content: Text(
          '"${m.title}"\n\nThe agent stops being able to recall it. This cannot '
          'be undone.',
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Fleet.bad),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Forget'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;

    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(apiProvider).forgetMemory(widget.instanceId, m.id);
      await _refresh();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('${widget.instanceName} · memory',
                style: const TextStyle(fontSize: 15)),
            Text(
              _loading
                  ? 'Loading…'
                  : '${_memories.length} kept',
              style: TextStyle(color: Fleet.ink400, fontSize: 11),
            ),
          ],
        ),
        actions: [
          IconButton(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh_rounded),
            onPressed: _refresh,
          ),
        ],
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
          child: Text('Could not load memory: $_error',
              textAlign: TextAlign.center,
              style: TextStyle(color: Fleet.bad, fontSize: 12)),
        ),
      );
    }
    if (_memories.isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.psychology_outlined, size: 34, color: Fleet.ink600),
              const SizedBox(height: 12),
              Text(
                'Nothing remembered yet.\n\nThis bot records something when it '
                'decides a finding is worth keeping across tasks.',
                textAlign: TextAlign.center,
                style:
                    TextStyle(color: Fleet.ink400, fontSize: 13, height: 1.4),
              ),
            ],
          ),
        ),
      );
    }

    return RefreshIndicator(
      onRefresh: _refresh,
      child: ListView.builder(
        padding: const EdgeInsets.all(12),
        itemCount: _memories.length,
        itemBuilder: (_, i) => _tile(_memories[i]),
      ),
    );
  }

  Widget _tile(BotMemory m) {
    return Card(
      color: Fleet.ink850,
      margin: const EdgeInsets.only(bottom: 8),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(12, 10, 6, 10),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(m.title,
                      style: const TextStyle(
                          fontSize: 13, fontWeight: FontWeight.w700)),
                  const SizedBox(height: 5),
                  Text(m.content,
                      style: const TextStyle(fontSize: 12, height: 1.35)),
                  const SizedBox(height: 7),
                  Text(
                    _stamp(m.createdAt),
                    style: TextStyle(color: Fleet.ink500, fontSize: 10),
                  ),
                ],
              ),
            ),
            IconButton(
              tooltip: 'Forget',
              icon: Icon(Icons.delete_outline, size: 18, color: Fleet.ink400),
              onPressed: () => _forget(m),
            ),
          ],
        ),
      ),
    );
  }

  String _stamp(DateTime t) {
    String two(int n) => n.toString().padLeft(2, '0');
    final local = t.toLocal();
    return '${local.year}-${two(local.month)}-${two(local.day)} '
        '${two(local.hour)}:${two(local.minute)}';
  }
}
