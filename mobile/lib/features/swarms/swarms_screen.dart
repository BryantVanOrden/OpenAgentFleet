import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import 'launch_swarm_sheet.dart';

/// Mission control: collaborative multi-bot swarms working a shared
/// blackboard, with peer-reviewed deliverables and an operator channel.
///
/// Reached from the Fleet app bar rather than a tab of its own — a swarm is a
/// way of running the fleet, not a separate place.
class SwarmsScreen extends ConsumerStatefulWidget {
  const SwarmsScreen({super.key});

  @override
  ConsumerState<SwarmsScreen> createState() => _SwarmsScreenState();
}

class _SwarmsScreenState extends ConsumerState<SwarmsScreen> {
  List<SwarmTeam> _swarms = const [];
  String _selectedId = '';
  bool _loading = true;
  String? _error;
  Timer? _poll;
  final _chat = TextEditingController();
  bool _sending = false;

  @override
  void initState() {
    super.initState();
    _refresh();
    // Swarm traffic has no websocket topic, so this polls — the same pattern
    // as fleet comms, and only while the screen is open.
    _poll = Timer.periodic(const Duration(seconds: 4), (_) => _refresh());
  }

  @override
  void dispose() {
    _poll?.cancel();
    _chat.dispose();
    super.dispose();
  }

  Future<void> _refresh() async {
    try {
      final list = await ref.read(apiProvider).swarms();
      if (!mounted) return;
      setState(() {
        _swarms = list;
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

  SwarmTeam? get _selected {
    if (_swarms.isEmpty) return null;
    return _swarms.where((s) => s.id == _selectedId).firstOrNull ??
        _swarms.first;
  }

  Future<void> _launch() async {
    final created = await LaunchSwarmSheet.show(context);
    if (created == null || !mounted) return;
    setState(() => _selectedId = created.id);
    await _refresh();
  }

  Future<void> _review(SwarmTeam swarm, SwarmArtifact art, bool approved) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(apiProvider).reviewSwarmArtifact(
            swarm.id,
            art.id,
            approved: approved,
            reviewer: 'operator',
            notes: approved
                ? 'approved in the OpenAgentFleet app'
                : 'rejected in the OpenAgentFleet app',
          );
      await _refresh();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  Future<void> _send(SwarmTeam swarm) async {
    final text = _chat.text.trim();
    if (text.isEmpty || _sending) return;
    setState(() => _sending = true);
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(apiProvider).postSwarmMessage(
            swarm.id,
            text,
            fromBot: 'Mission Operator',
            toBot: 'all',
            phase: 'execution',
          );
      _chat.clear();
      await _refresh();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Swarms'),
        actions: [
          IconButton(
            tooltip: 'Launch a swarm',
            icon: const Icon(Icons.rocket_launch_outlined),
            onPressed: _launch,
          ),
        ],
      ),
      body: _body(),
    );
  }

  Widget _body() {
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_error != null && _swarms.isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Text('Could not load swarms: $_error',
              textAlign: TextAlign.center,
              style: TextStyle(color: Fleet.bad, fontSize: 12)),
        ),
      );
    }
    final swarm = _selected;
    if (swarm == null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.diversity_3_outlined, size: 48, color: Fleet.ink600),
              const SizedBox(height: 16),
              const Text('No swarms yet', style: TextStyle(fontSize: 16)),
              const SizedBox(height: 6),
              Text(
                'A swarm is a team of your bots on one mission, sharing a '
                'blackboard and reviewing each other\'s work.',
                textAlign: TextAlign.center,
                style: TextStyle(color: Fleet.ink400, fontSize: 13),
              ),
              const SizedBox(height: 18),
              FilledButton.icon(
                onPressed: _launch,
                icon: const Icon(Icons.rocket_launch_outlined, size: 18),
                label: const Text('Launch a swarm'),
              ),
            ],
          ),
        ),
      );
    }

    return Column(
      children: [
        // Which mission you are looking at.
        SizedBox(
          height: 56,
          child: ListView(
            scrollDirection: Axis.horizontal,
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
            children: [
              for (final s in _swarms)
                Padding(
                  padding: const EdgeInsets.only(right: 8),
                  child: ChoiceChip(
                    selected: s.id == swarm.id,
                    onSelected: (_) => setState(() => _selectedId = s.id),
                    selectedColor: Fleet.live.withValues(alpha: 0.18),
                    backgroundColor: Fleet.ink850,
                    label: Text(
                      '${s.name} · ${s.members.length} '
                      'bot${s.members.length == 1 ? '' : 's'} · ${s.status}',
                      style: TextStyle(
                        fontSize: 12,
                        color: s.id == swarm.id ? Fleet.ink100 : Fleet.ink300,
                      ),
                    ),
                  ),
                ),
            ],
          ),
        ),
        Expanded(
          child: RefreshIndicator(
            onRefresh: _refresh,
            child: ListView(
              padding: const EdgeInsets.fromLTRB(16, 4, 16, 16),
              children: [
                _RosterCard(swarm: swarm),
                const SizedBox(height: 12),
                _ArtifactsCard(swarm: swarm, onReview: _review),
                const SizedBox(height: 12),
                _MessagesCard(swarm: swarm),
              ],
            ),
          ),
        ),
        // The operator's channel into the blackboard.
        SafeArea(
          top: false,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(12, 6, 12, 10),
            child: Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _chat,
                    enabled: !_sending,
                    textCapitalization: TextCapitalization.sentences,
                    decoration: const InputDecoration(
                      hintText: 'Broadcast a directive to the swarm…',
                      isDense: true,
                    ),
                    onSubmitted: (_) => _send(swarm),
                  ),
                ),
                const SizedBox(width: 8),
                IconButton.filled(
                  onPressed: _sending ? null : () => _send(swarm),
                  icon: const Icon(Icons.send, size: 18),
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

// -------------------------------------------------------------------- roster ---

class _RosterCard extends StatelessWidget {
  const _RosterCard({required this.swarm});
  final SwarmTeam swarm;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Expanded(
                  child: Text('Team roster',
                      style:
                          TextStyle(fontSize: 14, fontWeight: FontWeight.w600)),
                ),
                StateChip(state: swarm.status, live: swarm.status == 'running'),
              ],
            ),
            const SizedBox(height: 10),
            Container(
              width: double.infinity,
              padding: const EdgeInsets.all(10),
              decoration: BoxDecoration(
                color: Fleet.ink950,
                borderRadius: BorderRadius.circular(10),
                border: Border.all(color: Fleet.ink700),
              ),
              child: SelectableText(
                swarm.mission,
                style: TextStyle(
                  color: Fleet.ink300,
                  fontSize: 12,
                  fontFamily: 'monospace',
                  height: 1.4,
                ),
              ),
            ),
            const SizedBox(height: 10),
            for (final m in swarm.members)
              Container(
                margin: const EdgeInsets.only(bottom: 6),
                padding:
                    const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
                decoration: BoxDecoration(
                  color: Fleet.ink850,
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Row(
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(m.instanceName,
                              overflow: TextOverflow.ellipsis,
                              style: const TextStyle(
                                  fontSize: 13, fontWeight: FontWeight.w600)),
                          Text(m.role,
                              overflow: TextOverflow.ellipsis,
                              style: TextStyle(
                                  color: Fleet.ink400,
                                  fontSize: 11,
                                  fontFamily: 'monospace')),
                        ],
                      ),
                    ),
                    if (m.archetypeId.isNotEmpty)
                      Container(
                        padding: const EdgeInsets.symmetric(
                            horizontal: 7, vertical: 2),
                        decoration: BoxDecoration(
                          color: Fleet.cool.withValues(alpha: 0.15),
                          borderRadius: BorderRadius.circular(5),
                        ),
                        child: Text(m.archetypeId,
                            style: TextStyle(
                                color: Fleet.cool,
                                fontSize: 10,
                                fontFamily: 'monospace')),
                      ),
                  ],
                ),
              ),
          ],
        ),
      ),
    );
  }
}

// ----------------------------------------------------------------- artifacts ---

class _ArtifactsCard extends StatelessWidget {
  const _ArtifactsCard({required this.swarm, required this.onReview});
  final SwarmTeam swarm;
  final void Function(SwarmTeam, SwarmArtifact, bool) onReview;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('Shared artifacts',
                style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600)),
            const SizedBox(height: 8),
            if (swarm.artifacts.isEmpty)
              Text('Nothing published to the blackboard yet.',
                  style: TextStyle(
                      color: Fleet.ink500,
                      fontSize: 12,
                      fontStyle: FontStyle.italic))
            else
              for (final art in swarm.artifacts)
                Container(
                  margin: const EdgeInsets.only(bottom: 8),
                  padding: const EdgeInsets.all(10),
                  decoration: BoxDecoration(
                    color: Fleet.ink950,
                    borderRadius: BorderRadius.circular(10),
                    border: Border.all(color: Fleet.ink700),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Expanded(
                            child: Text(art.title,
                                style: const TextStyle(
                                    fontSize: 13,
                                    fontWeight: FontWeight.w600)),
                          ),
                          Text(art.category,
                              style: TextStyle(
                                  color: Fleet.ink400,
                                  fontSize: 10,
                                  fontFamily: 'monospace')),
                        ],
                      ),
                      const SizedBox(height: 4),
                      Text(
                        art.content,
                        maxLines: 4,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                            color: Fleet.ink400, fontSize: 12, height: 1.35),
                      ),
                      const SizedBox(height: 8),
                      Wrap(
                        spacing: 6,
                        runSpacing: 4,
                        crossAxisAlignment: WrapCrossAlignment.center,
                        children: [
                          Text('by ${art.author}',
                              style: TextStyle(
                                  color: Fleet.ink500,
                                  fontSize: 10,
                                  fontFamily: 'monospace')),
                          if (art.approvedBy.isEmpty)
                            Container(
                              padding: const EdgeInsets.symmetric(
                                  horizontal: 6, vertical: 2),
                              decoration: BoxDecoration(
                                color: Fleet.warn.withValues(alpha: 0.15),
                                borderRadius: BorderRadius.circular(5),
                              ),
                              child: Text('awaiting review',
                                  style: TextStyle(
                                      color: Fleet.warn,
                                      fontSize: 10,
                                      fontFamily: 'monospace')),
                            )
                          else
                            for (final who in art.approvedBy)
                              Container(
                                padding: const EdgeInsets.symmetric(
                                    horizontal: 6, vertical: 2),
                                decoration: BoxDecoration(
                                  color: Fleet.good.withValues(alpha: 0.15),
                                  borderRadius: BorderRadius.circular(5),
                                ),
                                child: Text('✓ $who',
                                    style: TextStyle(
                                        color: Fleet.good,
                                        fontSize: 10,
                                        fontFamily: 'monospace')),
                              ),
                        ],
                      ),
                      const SizedBox(height: 6),
                      Row(
                        mainAxisAlignment: MainAxisAlignment.end,
                        children: [
                          // The operator's own verdict, recorded as
                          // "operator" — a person signing off is a different
                          // fact from a peer bot signing off.
                          TextButton(
                            onPressed: () => onReview(swarm, art, true),
                            child: const Text('Approve'),
                          ),
                          TextButton(
                            style: TextButton.styleFrom(
                                foregroundColor: Fleet.bad),
                            onPressed: () => onReview(swarm, art, false),
                            child: const Text('Reject'),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
          ],
        ),
      ),
    );
  }
}

// ------------------------------------------------------------------ messages ---

class _MessagesCard extends StatelessWidget {
  const _MessagesCard({required this.swarm});
  final SwarmTeam swarm;

  static const _operator = 'Mission Operator';

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('Blackboard messages',
                style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600)),
            const SizedBox(height: 8),
            if (swarm.messages.isEmpty)
              Text('The team has not said anything yet.',
                  style: TextStyle(
                      color: Fleet.ink500,
                      fontSize: 12,
                      fontStyle: FontStyle.italic))
            else
              for (final msg in swarm.messages)
                Container(
                  margin: EdgeInsets.only(
                    bottom: 8,
                    // Operator messages sit visibly apart from the bots'.
                    left: msg.fromBot == _operator ? 24 : 0,
                    right: msg.fromBot == _operator ? 0 : 24,
                  ),
                  padding: const EdgeInsets.all(10),
                  decoration: BoxDecoration(
                    color: msg.fromBot == _operator
                        ? Fleet.live.withValues(alpha: 0.10)
                        : Fleet.ink950,
                    borderRadius: BorderRadius.circular(10),
                    border: Border.all(
                      color: msg.fromBot == _operator
                          ? Fleet.live.withValues(alpha: 0.3)
                          : Fleet.ink700,
                    ),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Expanded(
                            child: Text(
                              '${msg.fromBot} ➔ ${msg.toBot}',
                              overflow: TextOverflow.ellipsis,
                              style: TextStyle(
                                color: Fleet.ink200,
                                fontSize: 10.5,
                                fontWeight: FontWeight.w700,
                                fontFamily: 'monospace',
                              ),
                            ),
                          ),
                          if (msg.phase.isNotEmpty)
                            Container(
                              padding: const EdgeInsets.symmetric(
                                  horizontal: 6, vertical: 2),
                              decoration: BoxDecoration(
                                color: Fleet.ink800,
                                borderRadius: BorderRadius.circular(5),
                              ),
                              child: Text(msg.phase,
                                  style: TextStyle(
                                      color: Fleet.ink400,
                                      fontSize: 10,
                                      fontFamily: 'monospace')),
                            ),
                        ],
                      ),
                      const SizedBox(height: 4),
                      SelectableText(msg.content,
                          style: TextStyle(
                              color: Fleet.ink200,
                              fontSize: 12,
                              height: 1.35)),
                      const SizedBox(height: 2),
                      Text(humanAgo(msg.createdAt),
                          style:
                              TextStyle(color: Fleet.ink500, fontSize: 10)),
                    ],
                  ),
                ),
          ],
        ),
      ),
    );
  }
}
