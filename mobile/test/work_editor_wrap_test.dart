import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:agentfleet_companion/core/models.dart';
import 'package:agentfleet_companion/core/network/api_client.dart';
import 'package:agentfleet_companion/core/theme/theme.dart';
import 'package:agentfleet_companion/features/vault/work_editor_screen.dart';

/// The editor put its field inside a horizontally scrolling viewport so long
/// lines could be read without wrapping. A horizontal viewport hands its child
/// an unbounded width, so nothing ever wrapped: one long line ran off the side
/// of the screen forever and there was no way to read the end of it.
void main() {
  testWidgets('a long line wraps instead of running off the screen',
      (tester) async {
    SharedPreferences.setMockInitialValues({});
    final api = await ApiClient.create();

    final oneLongLine = List.filled(60, 'const somethingRatherLong = 1;').join(' ');
    final item = WorkItem(
      id: 'w1',
      name: 'wide.js',
      kind: WorkItem.kindFile,
      version: 1,
      updatedAt: DateTime(2026, 8, 31),
      content: oneLongLine,
    );

    await tester.pumpWidget(MaterialApp(
      theme: buildTheme(),
      home: WorkEditorScreen(api: api, item: item),
    ));
    await tester.pumpAndSettle();

    final field = find.byType(TextField);
    expect(field, findsOneWidget);

    // Assert the shape of the bug, not a symptom of it.
    //
    // Measuring the field's width does not discriminate: under a test surface
    // the laid-out size comes back clamped either way, so a check on it passes
    // with the fault present and proves nothing. What was actually wrong is
    // structural — the field sat inside a viewport that scrolls sideways, and
    // such a viewport gives its child unbounded width, which is why nothing
    // ever wrapped.
    final horizontal = find.byWidgetPredicate(
      (w) => w is Scrollable && w.axis == Axis.horizontal,
      description: 'a horizontally scrolling viewport',
    );
    expect(
      horizontal,
      findsNothing,
      reason: 'the editor is inside a horizontal viewport again, so its field '
          'has unbounded width and long lines will not wrap',
    );

    // And the field must be free to grow downwards as text wraps.
    final widget = tester.widget<TextField>(field);
    expect(widget.maxLines, isNull,
        reason: 'a fixed maxLines stops the field wrapping onto more lines');
  });
}
