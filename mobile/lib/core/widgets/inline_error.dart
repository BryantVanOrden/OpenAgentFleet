import 'package:flutter/material.dart';

import '../theme/theme.dart';

/// An error shown in place.
///
/// Exists because a snackbar raised from inside a modal bottom sheet renders
/// *behind* the sheet. Every failure reported that way is invisible, and an
/// action that failed silently is indistinguishable from a control that does
/// nothing — which is exactly how it gets reported.
class InlineError extends StatelessWidget {
  const InlineError(this.message, {super.key});

  /// Null renders nothing, so callers can pass a nullable field directly.
  final String? message;

  @override
  Widget build(BuildContext context) {
    final text = message;
    if (text == null || text.isEmpty) return const SizedBox.shrink();

    return Container(
      width: double.infinity,
      margin: const EdgeInsets.only(top: 12),
      padding: const EdgeInsets.all(11),
      decoration: BoxDecoration(
        color: Fleet.bad.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(9),
        border: Border.all(color: Fleet.bad.withValues(alpha: 0.4)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(Icons.error_outline, size: 15, color: Fleet.bad),
          const SizedBox(width: 8),
          Expanded(
            child: Text(text,
                style: TextStyle(
                    color: Fleet.ink200, fontSize: 11.5, height: 1.4)),
          ),
        ],
      ),
    );
  }
}
