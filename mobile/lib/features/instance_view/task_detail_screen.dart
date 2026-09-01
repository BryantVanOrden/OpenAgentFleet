import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';

/// One run, step by step: what the agent saw, thought, did, and what came of
/// it. The mobile mirror of the console's activity pane, for reading a run
/// from wherever the alert found you.
class TaskDetailScreen extends ConsumerStatefulWidget {
  const TaskDetailScreen({super.key, required this.task});

  /// The run as it looked when opened. Live state comes from [tasksProvider],
  /// which re-reads on every task event for this instance; this snapshot is
  /// only the fallback while that loads.
  final Task task;

  @override
  ConsumerState<TaskDetailScreen> createState() => _TaskDetailScreenState();
}

class _TaskDetailScreenState extends ConsumerState<TaskDetailScreen> {
  List<StepRecord> _steps = const [];
  bool _loading = true;
  bool _fetching = false;
  String? _error;
  final _scroll = ScrollController();

  @override
  void initState() {
    super.initState();
    _loadSteps();
  }

  @override
  void dispose() {
    _scroll.dispose();
    super.dispose();
  }

  Future<void> _loadSteps() async {
    // The task-event listener fires for every task on the instance, so bursts
    // of events must not stack requests for the same list.
    if (_fetching) return;
    _fetching = true;
    try {
      final steps = await ref.read(apiProvider).taskSteps(widget.task.id);
      if (!mounted) return;
      final grew = steps.length > _steps.length;
      setState(() {
        _steps = steps;
        _loading = false;
        _error = null;
      });
      if (grew) _scrollToNewest();
    } catch (err) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = '$err';
      });
    } finally {
      _fetching = false;
    }
  }

  /// The newest step is the one you came to see; the history above it is
  /// context, reachable by scrolling up.
  void _scrollToNewest() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || !_scroll.hasClients) return;
      _scroll.animateTo(
        _scroll.position.maxScrollExtent,
        duration: const Duration(milliseconds: 300),
        curve: Curves.easeOut,
      );
    });
  }

  void _openImage(String url, int step) {
    Navigator.of(context).push(MaterialPageRoute(
      builder: (_) => _FullScreenshot(url: url, step: step),
    ));
  }

  @override
  Widget build(BuildContext context) {
    // tasksProvider re-yields on every task.* websocket event for this
    // instance — the same signal the console uses — so listening to it keeps
    // the timeline current without a timer.
    ref.listen(tasksProvider(widget.task.instanceId), (_, __) => _loadSteps());

    final tasks =
        ref.watch(tasksProvider(widget.task.instanceId)).valueOrNull ??
            const <Task>[];
    final task =
        tasks.where((t) => t.id == widget.task.id).firstOrNull ?? widget.task;

    final api = ref.read(apiProvider);
    final totalTokens =
        _steps.fold<int>(0, (n, s) => n + s.promptTokens + s.outputTokens);

    return Scaffold(
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('Run'),
            Text(
              task.goal,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(fontSize: 11, color: Fleet.ink400),
            ),
          ],
        ),
        actions: [
          Padding(
            padding: const EdgeInsets.only(right: 12),
            child: Center(
                child: StateChip(state: task.state, live: task.isLive)),
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: _loadSteps,
        child: _loading
            ? const Center(child: CircularProgressIndicator())
            : ListView(
                controller: _scroll,
                padding: const EdgeInsets.fromLTRB(16, 12, 16, 24),
                children: [
                  _HeaderCard(task: task, totalTokens: totalTokens),
                  if (_error != null)
                    Padding(
                      padding: const EdgeInsets.only(top: 12),
                      child: Text('Could not load steps: $_error',
                          style: TextStyle(color: Fleet.bad, fontSize: 12)),
                    ),
                  const SizedBox(height: 12),
                  if (_steps.isEmpty && _error == null)
                    Padding(
                      padding: const EdgeInsets.all(24),
                      child: Text(
                        'No steps recorded yet. They appear here as the '
                        'agent works.',
                        textAlign: TextAlign.center,
                        style: TextStyle(color: Fleet.ink400, fontSize: 13),
                      ),
                    ),
                  for (final s in _steps)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 10),
                      child: StepTimelineTile(
                        step: s,
                        imageUrl: s.observationKey.isEmpty
                            ? null
                            : api.artifactUrl(s.observationKey),
                        onImageTap: s.observationKey.isEmpty
                            ? null
                            : () => _openImage(
                                api.artifactUrl(s.observationKey), s.step),
                      ),
                    ),
                ],
              ),
      ),
    );
  }
}

class _HeaderCard extends StatelessWidget {
  const _HeaderCard({required this.task, required this.totalTokens});

  final Task task;
  final int totalTokens;

  @override
  Widget build(BuildContext context) {
    final mono = TextStyle(
      color: Fleet.ink400,
      fontSize: 11,
      fontFamily: 'monospace',
    );

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SelectableText(task.goal, style: const TextStyle(fontSize: 14)),
            const SizedBox(height: 10),
            Row(
              children: [
                Expanded(
                    child: Text('step ${task.step}/${task.maxSteps}',
                        style: mono)),
                Expanded(
                    child: Text('started ${humanAgo(task.createdAt)}',
                        style: mono, textAlign: TextAlign.center)),
                Expanded(
                    child: Text('tokens $totalTokens',
                        style: mono, textAlign: TextAlign.right)),
              ],
            ),
            if (task.error.isNotEmpty) ...[
              const SizedBox(height: 10),
              SelectableText(task.error,
                  style: TextStyle(color: Fleet.bad, fontSize: 12)),
            ],
            if (task.result.isNotEmpty) ...[
              const SizedBox(height: 10),
              SelectableText(task.result,
                  style: TextStyle(color: Fleet.good, fontSize: 12)),
            ],
          ],
        ),
      ),
    );
  }
}

/// One step of the timeline. Public and self-contained — it takes the image
/// URL rather than reading the API client — so it can be rendered and tested
/// without a provider scope.
class StepTimelineTile extends StatelessWidget {
  const StepTimelineTile({
    super.key,
    required this.step,
    this.imageUrl,
    this.onImageTap,
  });

  final StepRecord step;

  /// Where the step's screenshot lives, or null for a step with no frame.
  final String? imageUrl;
  final VoidCallback? onImageTap;

  @override
  Widget build(BuildContext context) {
    final action = step.action;

    return Card(
      color: Fleet.ink850,
      child: Padding(
        padding: const EdgeInsets.all(10),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _Thumbnail(url: imageUrl, onTap: onImageTap),
            const SizedBox(width: 10),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Wrap(
                    spacing: 6,
                    runSpacing: 4,
                    crossAxisAlignment: WrapCrossAlignment.center,
                    children: [
                      Text('#${step.step}',
                          style: TextStyle(
                              color: Fleet.ink500,
                              fontSize: 11,
                              fontFamily: 'monospace')),
                      _Badge(text: action.action, color: Fleet.live),
                      if (action.mark != 0)
                        _Badge(text: 'Mark [${action.mark}]', color: Fleet.good),
                      if (action.query.isNotEmpty)
                        _Badge(text: action.query, color: Fleet.cool),
                      Text(
                        '${(step.durationMs / 1000).toStringAsFixed(1)}s',
                        style: TextStyle(
                            color: Fleet.ink500,
                            fontSize: 11,
                            fontFamily: 'monospace'),
                      ),
                    ],
                  ),
                  if (action.detail.isNotEmpty) ...[
                    const SizedBox(height: 4),
                    Text(
                      action.detail,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                          color: Fleet.ink300,
                          fontSize: 11,
                          fontFamily: 'monospace'),
                    ),
                  ],
                  if (action.thought.isNotEmpty) ...[
                    const SizedBox(height: 4),
                    Text(
                      '“${action.thought}”',
                      style: TextStyle(
                        color: Fleet.ink200,
                        fontSize: 12.5,
                        fontStyle: FontStyle.italic,
                        height: 1.35,
                      ),
                    ),
                  ],
                  if (step.outcome.isNotEmpty) ...[
                    const SizedBox(height: 4),
                    Text(
                      step.outcome,
                      style: TextStyle(
                        // Same failure heuristic as the console, so a step
                        // that looks alarming there looks alarming here.
                        color: step.failed ? Fleet.bad : Fleet.ink400,
                        fontSize: 11,
                        fontFamily: 'monospace',
                        height: 1.35,
                      ),
                    ),
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _Badge extends StatelessWidget {
  const _Badge({required this.text, required this.color});
  final String text;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.15),
        borderRadius: BorderRadius.circular(5),
        border: Border.all(color: color.withValues(alpha: 0.3)),
      ),
      child: Text(
        text,
        style: TextStyle(color: color, fontSize: 10.5, fontFamily: 'monospace'),
      ),
    );
  }
}

class _Thumbnail extends StatelessWidget {
  const _Thumbnail({this.url, this.onTap});
  final String? url;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final placeholder = Container(
      width: 96,
      height: 60,
      decoration: BoxDecoration(
        color: Fleet.ink800,
        borderRadius: BorderRadius.circular(8),
      ),
      alignment: Alignment.center,
      child: Text('no frame',
          style: TextStyle(color: Fleet.ink500, fontSize: 10)),
    );

    final u = url;
    if (u == null) return placeholder;

    return GestureDetector(
      onTap: onTap,
      child: ClipRRect(
        borderRadius: BorderRadius.circular(8),
        child: Image.network(
          u,
          width: 96,
          height: 60,
          fit: BoxFit.cover,
          // A missing artifact renders as the same "no frame" box rather than
          // a broken-image glyph.
          errorBuilder: (_, __, ___) => placeholder,
        ),
      ),
    );
  }
}

/// A step's screenshot at full size, pinch-zoomable.
class _FullScreenshot extends StatelessWidget {
  const _FullScreenshot({required this.url, required this.step});
  final String url;
  final int step;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(
        backgroundColor: Colors.black,
        title: Text('Step $step'),
      ),
      body: Center(
        child: InteractiveViewer(
          maxScale: 5,
          child: Image.network(
            url,
            fit: BoxFit.contain,
            errorBuilder: (_, __, ___) => Text(
              'Could not load this frame.',
              style: TextStyle(color: Fleet.ink400, fontSize: 13),
            ),
          ),
        ),
      ),
    );
  }
}
