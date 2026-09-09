import 'package:flutter/material.dart';

import '../../core/models.dart';
import '../../core/theme/theme.dart';

/// First-run setup, done for you.
///
/// Sits at the top of the fleet chat until a model is connected, a bot exists
/// and something has been asked of it. One step at a time, one button each:
/// the button runs the fleet command that does the work (`/setup`, `/new`), so
/// this is a front for verbs the chat already understands, not a second setup
/// flow to keep in sync with the console.
class SetupCard extends StatefulWidget {
  const SetupCard({
    super.key,
    required this.status,
    required this.busy,
    required this.readOnly,
    required this.onRun,
    required this.onFocusComposer,
    required this.leading,
  });

  final SetupStatus status;
  final bool busy;
  final bool readOnly;

  /// Runs a slash command exactly as the composer would.
  final ValueChanged<String> onRun;
  final VoidCallback onFocusComposer;

  /// The mascot, supplied by the screen so this file owns no assets.
  final Widget leading;

  @override
  State<SetupCard> createState() => _SetupCardState();
}

class _SetupCardState extends State<SetupCard> {
  final _address = TextEditingController();
  bool _showAddress = false;

  @override
  void dispose() {
    _address.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final steps = widget.status.steps;
    final current = steps.where((s) => !s.done).firstOrNull;
    if (current == null) return const SizedBox.shrink();
    final index = steps.indexOf(current);
    final enabled = !widget.busy && !widget.readOnly;

    final (label, run) = switch (current.id) {
      'model' => ('Find my model', () => widget.onRun('/setup')),
      'bot' => ('Create a bot', () => widget.onRun('/new fullstack_dev Scout')),
      _ => ('Tell it what to do', widget.onFocusComposer),
    };

    return Container(
      margin: const EdgeInsets.fromLTRB(12, 12, 12, 4),
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Fleet.ink900,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: Fleet.ink700),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              widget.leading,
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      'STEP ${index + 1} OF ${steps.length}',
                      style: TextStyle(
                        fontSize: 10,
                        letterSpacing: 1.6,
                        fontWeight: FontWeight.w600,
                        color: Fleet.ink400,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(current.title,
                        style: const TextStyle(
                            fontSize: 17, fontWeight: FontWeight.w600)),
                    if (current.hint.isNotEmpty) ...[
                      const SizedBox(height: 4),
                      Text(current.hint,
                          style: TextStyle(
                              fontSize: 13, height: 1.4, color: Fleet.ink300)),
                    ],
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 14),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              FilledButton(
                onPressed: enabled ? run : null,
                child: Text(widget.busy ? 'Working…' : label),
              ),
              if (current.id == 'model' && !_showAddress)
                TextButton(
                  onPressed:
                      enabled ? () => setState(() => _showAddress = true) : null,
                  child: const Text('I have an address'),
                ),
            ],
          ),
          if (current.id == 'model' && _showAddress) ...[
            const SizedBox(height: 10),
            Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _address,
                    autofocus: true,
                    keyboardType: TextInputType.url,
                    style: const TextStyle(fontSize: 12, fontFamily: 'monospace'),
                    decoration: const InputDecoration(
                      isDense: true,
                      hintText: 'http://192.168.1.20:11434',
                    ),
                    onSubmitted: (_) => _connect(),
                  ),
                ),
                const SizedBox(width: 8),
                FilledButton.tonal(
                  onPressed: enabled ? _connect : null,
                  child: const Text('Connect'),
                ),
              ],
            ),
          ],
          const SizedBox(height: 16),
          Row(
            children: [
              for (var i = 0; i < steps.length; i++) ...[
                Expanded(
                  child: AnimatedContainer(
                    duration: const Duration(milliseconds: 240),
                    height: 4,
                    decoration: BoxDecoration(
                      borderRadius: BorderRadius.circular(2),
                      color: steps[i].done
                          ? Fleet.live
                          : i == index
                              ? Fleet.live.withValues(alpha: 0.4)
                              : Fleet.ink700,
                    ),
                  ),
                ),
                if (i < steps.length - 1) const SizedBox(width: 6),
              ],
            ],
          ),
          const SizedBox(height: 6),
          Row(
            children: [
              for (var i = 0; i < steps.length; i++)
                Expanded(
                  child: Text(
                    '${steps[i].done ? '✓ ' : ''}${steps[i].title}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: 10,
                      color: steps[i].done
                          ? Fleet.ink300
                          : i == index
                              ? Fleet.ink100
                              : Fleet.ink500,
                    ),
                  ),
                ),
            ],
          ),
        ],
      ),
    );
  }

  void _connect() {
    final url = _address.text.trim();
    if (url.isEmpty) return;
    widget.onRun('/setup $url');
  }
}
