import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import 'skill_detail_screen.dart';

/// Recorded skills: demonstrations compiled into instructions.
///
/// Reachable from the Fleet app bar rather than Admin — operators record and
/// use skills as part of driving the fleet; nothing here changes who may do
/// what.
class SkillsScreen extends ConsumerStatefulWidget {
  const SkillsScreen({super.key});

  @override
  ConsumerState<SkillsScreen> createState() => _SkillsScreenState();
}

class _SkillsScreenState extends ConsumerState<SkillsScreen> {
  List<Skill> _skills = const [];
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
      final list = await ref.read(apiProvider).skills();
      if (mounted) setState(() => _skills = list);
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
        title: const Text('Skills'),
        actions: [
          IconButton(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: _load,
          ),
        ],
      ),
      body: _loading && _skills.isEmpty
          ? const Center(child: CircularProgressIndicator())
          : _error != null && _skills.isEmpty
              ? Center(
                  child: Text(_error!, style: TextStyle(color: Fleet.bad)))
              : RefreshIndicator(
                  onRefresh: _load,
                  child: ListView(
                    padding: const EdgeInsets.fromLTRB(16, 12, 16, 24),
                    children: [
                      Text(
                        'A raw recording is always noisier than the task it '
                        'represents. This is where you prune it down to the '
                        'procedure you meant to demonstrate and mark the '
                        'values that should vary between runs.',
                        style: TextStyle(
                            color: Fleet.ink400, fontSize: 12, height: 1.4),
                      ),
                      const SizedBox(height: 12),
                      if (_skills.isEmpty && !_loading)
                        Padding(
                          padding: const EdgeInsets.all(32),
                          child: Text(
                            'No skills yet.\n\nRecord one from an instance: '
                            'open a bot, start the recording studio, and '
                            'demonstrate the task by hand.',
                            textAlign: TextAlign.center,
                            style:
                                TextStyle(color: Fleet.ink400, height: 1.4),
                          ),
                        ),
                      for (final s in _skills)
                        Card(
                          color: Fleet.ink850,
                          margin: const EdgeInsets.only(bottom: 8),
                          child: ListTile(
                            leading: Icon(Icons.psychology_outlined,
                                color: Fleet.ink300),
                            title: Row(
                              children: [
                                Flexible(
                                  child: Text(s.name,
                                      overflow: TextOverflow.ellipsis),
                                ),
                                const SizedBox(width: 8),
                                Container(
                                  padding: const EdgeInsets.symmetric(
                                      horizontal: 7, vertical: 1),
                                  decoration: BoxDecoration(
                                    color: Fleet.live.withValues(alpha: 0.12),
                                    borderRadius: BorderRadius.circular(999),
                                    border: Border.all(
                                        color: Fleet.live
                                            .withValues(alpha: 0.25)),
                                  ),
                                  child: Text('v${s.version}',
                                      style: TextStyle(
                                          fontFamily: 'monospace',
                                          fontSize: 10,
                                          color: Fleet.live)),
                                ),
                              ],
                            ),
                            subtitle: Text(
                              '${s.stepCount} steps'
                              '${s.params.isNotEmpty ? ' · ${s.params.length} params' : ''}',
                              style: TextStyle(
                                  fontFamily: 'monospace',
                                  color: Fleet.ink400,
                                  fontSize: 11),
                            ),
                            trailing: const Icon(Icons.chevron_right),
                            onTap: () async {
                              await Navigator.of(context).push(
                                MaterialPageRoute(
                                    builder: (_) =>
                                        SkillDetailScreen(skill: s)),
                              );
                              _load();
                            },
                          ),
                        ),
                    ],
                  ),
                ),
    );
  }
}
