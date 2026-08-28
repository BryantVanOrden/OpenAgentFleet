import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';

class SwarmsScreen extends ConsumerStatefulWidget {
  const SwarmsScreen({super.key});

  @override
  ConsumerState<SwarmsScreen> createState() => _SwarmsScreenState();
}

class _SwarmsScreenState extends ConsumerState<SwarmsScreen> {
  List<SwarmTeam> _swarms = [];
  SwarmTeam? _selected;
  bool _loading = false;
  String? _error;
  final _messageController = TextEditingController();

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _messageController.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final client = ref.read(apiClientProvider);
    if (client == null) return;
    setState(() => _loading = true);
    try {
      final list = await client.swarms();
      setState(() {
        _swarms = list;
        if (list.isNotEmpty && _selected == null) {
          _selected = list.first;
        } else if (_selected != null) {
          _selected = list.firstWhere((s) => s.id == _selected!.id, orElse: () => list.first);
        }
        _error = null;
      });
    } catch (e) {
      setState(() => _error = e.toString());
    } finally {
      setState(() => _loading = false);
    }
  }

  Future<void> _sendMessage() async {
    final text = _messageController.text.trim();
    if (text.isEmpty || _selected == null) return;
    final client = ref.read(apiClientProvider);
    if (client == null) return;

    _messageController.clear();
    try {
      await client.postSwarmMessage(_selected!.id, text);
      await _load();
    } catch (e) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Failed to send message: $e')),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(
        title: const Text('🐝 Swarm Mission Control'),
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh),
            onPressed: _load,
          ),
        ],
      ),
      body: _loading && _swarms.isEmpty
          ? const Center(child: CircularProgressIndicator())
          : _swarms.isEmpty
              ? Center(
                  child: Padding(
                    padding: const EdgeInsets.all(24),
                    child: Column(
                      mainAxisAlignment: MainAxisAlignment.center,
                      children: [
                        const Icon(Icons.hub_outlined, size: 64, color: Colors.grey),
                        const SizedBox(height: 16),
                        Text(
                          'No Active Swarms',
                          style: theme.textTheme.titleMedium,
                        ),
                        const SizedBox(height: 8),
                        const Text(
                          'Launch a collaborative multi-bot team from the Admin Console to track live execution here.',
                          textAlign: TextAlign.center,
                          style: TextStyle(color: Colors.grey),
                        ),
                      ],
                    ),
                  ),
                )
              : Column(
                  children: [
                    // Swarm Selector Chips
                    Container(
                      height: 54,
                      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                      child: ListView.separated(
                        scrollDirection: Axis.horizontal,
                        itemCount: _swarms.length,
                        separatorBuilder: (_, __) => const SizedBox(width: 8),
                        itemBuilder: (context, i) {
                          final sw = _swarms[i];
                          final isSelected = sw.id == _selected?.id;
                          return ChoiceChip(
                            label: Text('${sw.name} (${sw.members.length} bots)'),
                            selected: isSelected,
                            onSelected: (_) => setState(() => _selected = sw),
                          );
                        },
                      ),
                    ),
                    const Divider(height: 1),

                    if (_selected != null) ...[
                      // Mission Header
                      Container(
                        padding: const EdgeInsets.all(12),
                        color: theme.colorScheme.surfaceContainerHighest.withOpacity(0.3),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Row(
                              mainAxisAlignment: MainAxisAlignment.spaceBetween,
                              children: [
                                Expanded(
                                  child: Text(
                                    _selected!.name,
                                    style: const TextStyle(fontWeight: FontWeight.bold),
                                  ),
                                ),
                                Container(
                                  padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                                  decoration: BoxDecoration(
                                    color: Colors.green.withOpacity(0.2),
                                    borderRadius: BorderRadius.circular(12),
                                  ),
                                  child: Text(
                                    _selected!.status.toUpperCase(),
                                    style: const TextStyle(
                                      fontSize: 10,
                                      fontWeight: FontWeight.bold,
                                      color: Colors.green,
                                    ),
                                  ),
                                ),
                              ],
                            ),
                            const SizedBox(height: 4),
                            Text(
                              '🎯 ${_selected!.mission}',
                              style: theme.textTheme.bodySmall,
                            ),
                          ],
                        ),
                      ),

                      // Blackboard Messages
                      Expanded(
                        child: ListView.builder(
                          padding: const EdgeInsets.all(12),
                          itemCount: _selected!.messages.length,
                          itemBuilder: (context, i) {
                            final msg = _selected!.messages[i];
                            final isOperator = msg.fromBot.contains('Operator');
                            return Align(
                              alignment: isOperator ? Alignment.centerRight : Alignment.centerLeft,
                              child: Container(
                                margin: const EdgeInsets.only(bottom: 8),
                                padding: const EdgeInsets.all(10),
                                constraints: BoxConstraints(
                                  maxWidth: MediaQuery.of(context).size.width * 0.82,
                                ),
                                decoration: BoxDecoration(
                                  color: isOperator
                                      ? theme.colorScheme.primaryContainer
                                      : theme.colorScheme.surfaceContainerHighest,
                                  borderRadius: BorderRadius.circular(12),
                                  border: Border.all(
                                    color: isOperator
                                        ? theme.colorScheme.primary.withOpacity(0.3)
                                        : Colors.transparent,
                                  ),
                                ),
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Row(
                                      mainAxisAlignment: MainAxisAlignment.spaceBetween,
                                      children: [
                                        Text(
                                          '${msg.fromBot} ➔ ${msg.toBot}',
                                          style: TextStyle(
                                            fontSize: 11,
                                            fontWeight: FontWeight.bold,
                                            color: theme.colorScheme.primary,
                                          ),
                                        ),
                                        Text(
                                          msg.phase,
                                          style: const TextStyle(fontSize: 10, color: Colors.grey),
                                        ),
                                      ],
                                    ),
                                    const SizedBox(height: 4),
                                    Text(msg.content, style: const TextStyle(fontSize: 13)),
                                  ],
                                ),
                              ),
                            );
                          },
                        ),
                      ),

                      // Input Bar
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
                        decoration: BoxDecoration(
                          color: theme.colorScheme.surface,
                          border: Border(top: BorderSide(color: theme.dividerColor)),
                        ),
                        child: Row(
                          children: [
                            Expanded(
                              child: TextField(
                                controller: _messageController,
                                decoration: const InputDecoration(
                                  hintText: 'Inject directive to swarm…',
                                  border: InputBorder.none,
                                  isDense: true,
                                ),
                                onSubmitted: (_) => _sendMessage(),
                              ),
                            ),
                            IconButton(
                              icon: const Icon(Icons.send),
                              onPressed: _sendMessage,
                            ),
                          ],
                        ),
                      ),
                    ],
                  ],
                ),
    );
  }
}
