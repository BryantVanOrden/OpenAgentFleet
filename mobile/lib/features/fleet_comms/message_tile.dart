import 'package:flutter/material.dart';

import '../../core/models.dart';
import '../../core/theme/theme.dart';

/// One message in a thread.
class MessageTile extends StatelessWidget {
  const MessageTile({super.key, required this.message, this.speaking = false});

  final PeerMessage message;

  /// Highlighted while this message is being read aloud, so you can tell which
  /// line the voice you are hearing belongs to.
  final bool speaking;

  @override
  Widget build(BuildContext context) {
    // Operator messages are the ones you sent; aligning them like your own
    // chat makes the thread readable at a glance.
    final mine = message.fromInstanceId.isEmpty;
    final summary = message.kind == 'summary';

    final kindColour = switch (message.kind) {
      'delegation' => Fleet.warn,
      'question' => Fleet.cool,
      'summary' => Fleet.cool,
      _ => Fleet.ink300,
    };

    // A summary stands for everything it replaced, so it reads as a marker
    // across the thread rather than as one participant's remark.
    if (summary) {
      return Container(
        margin: const EdgeInsets.only(bottom: 10),
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
        decoration: BoxDecoration(
          color: Fleet.ink850,
          borderRadius: BorderRadius.circular(10),
          border: Border.all(color: Fleet.cool.withValues(alpha: 0.35)),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.compress_rounded, size: 13, color: Fleet.cool),
                const SizedBox(width: 6),
                Text(
                  'COMPACTED'
                  '${message.compactedCount > 0 ? ' · ${message.compactedCount} messages' : ''}',
                  style: TextStyle(
                      fontSize: 9,
                      letterSpacing: 0.6,
                      fontWeight: FontWeight.w700,
                      color: Fleet.cool),
                ),
              ],
            ),
            const SizedBox(height: 6),
            Text(message.content,
                style: const TextStyle(fontSize: 13, height: 1.35)),
          ],
        ),
      );
    }

    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Row(
        mainAxisAlignment:
            mine ? MainAxisAlignment.end : MainAxisAlignment.start,
        children: [
          Flexible(
            child: AnimatedContainer(
              duration: const Duration(milliseconds: 180),
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
              decoration: BoxDecoration(
                color: mine ? Fleet.live.withValues(alpha: 0.14) : Fleet.ink800,
                borderRadius: BorderRadius.circular(12),
                border: speaking
                    ? Border.all(color: Fleet.good)
                    : mine
                        ? Border.all(color: Fleet.live.withValues(alpha: 0.3))
                        : null,
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Flexible(
                        child: Text(
                          message.fromInstanceName,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                              fontSize: 11, fontWeight: FontWeight.w700),
                        ),
                      ),
                      if (speaking) ...[
                        const SizedBox(width: 5),
                        Icon(Icons.graphic_eq_rounded,
                            size: 11, color: Fleet.good),
                      ],
                      const SizedBox(width: 6),
                      Container(
                        padding: const EdgeInsets.symmetric(
                            horizontal: 5, vertical: 1),
                        decoration: BoxDecoration(
                          color: kindColour.withValues(alpha: 0.16),
                          borderRadius: BorderRadius.circular(4),
                        ),
                        child: Text(message.kind,
                            style: TextStyle(fontSize: 9, color: kindColour)),
                      ),
                    ],
                  ),
                  const SizedBox(height: 5),
                  Text(message.content,
                      style: const TextStyle(fontSize: 13, height: 1.35)),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}
