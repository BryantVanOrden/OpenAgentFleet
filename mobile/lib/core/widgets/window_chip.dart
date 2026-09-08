import 'package:flutter/material.dart';

import '../theme/theme.dart';

/// Marks a task as one step-window of a marathon.
///
/// A long run is split into windows, each a task of its own that carries on
/// where the last left off. Without this a marathon reads as several
/// unrelated tasks with the same goal.
class WindowChip extends StatelessWidget {
  const WindowChip(this.window, {super.key});

  final int window;

  @override
  Widget build(BuildContext context) {
    if (window <= 0) return const SizedBox.shrink();
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
      decoration: BoxDecoration(
        color: Fleet.cool.withValues(alpha: 0.14),
        borderRadius: BorderRadius.circular(4),
      ),
      child: Text(
        '↻ window $window',
        style: TextStyle(
            fontSize: 10, color: Fleet.cool, fontFamily: 'monospace'),
      ),
    );
  }
}
