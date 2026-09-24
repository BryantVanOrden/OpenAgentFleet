import 'package:agentfleet_companion/core/theme/theme.dart';
import 'package:agentfleet_companion/features/settings/support_card.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

// Typed out here on purpose rather than read from SupportCard: a slip in the
// card's constants must fail this test, not agree with it.
const _xrp = 'rf82s1CDagppvM6ATqc1nSrL6GackzHJrm';
const _tag = '796343731';
const _btc = 'bc1qvre807vxh08puxwc2z5adnm59tta7v5mqmky45';

Widget _wrap(Brightness b) => MaterialApp(
      theme: buildTheme(b, FleetAccent.amber),
      home: const Scaffold(
        body: SingleChildScrollView(
          padding: EdgeInsets.all(16),
          child: SupportCard(),
        ),
      ),
    );

Future<void> _pumpAt(WidgetTester tester, Size size, Brightness b) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(_wrap(b));
}

void main() {
  for (final b in Brightness.values) {
    testWidgets('shows both addresses and the tag, with copy buttons ($b)',
        (tester) async {
      final semantics = tester.ensureSemantics();
      await _pumpAt(tester, const Size(390, 1800), b);

      expect(find.text('Support this project'), findsOneWidget);
      expect(find.text(SupportCard.intro), findsOneWidget);
      expect(find.text(_xrp), findsOneWidget);
      expect(find.text(_tag), findsOneWidget);
      expect(find.text(_btc), findsOneWidget);
      expect(
        find.text('A destination tag is required when sending XRP to this '
            'address (796343731). Without it the transfer will not be '
            'credited.'),
        findsOneWidget,
      );
      expect(find.byType(SelectableText), findsNWidgets(3));

      expect(find.byTooltip('Copy XRP address'), findsOneWidget);
      expect(find.byTooltip('Copy XRP destination tag'), findsOneWidget);
      expect(find.byTooltip('Copy Bitcoin address'), findsOneWidget);
      expect(find.bySemanticsLabel('Copy XRP address'), findsOneWidget);
      expect(find.bySemanticsLabel('Copy XRP destination tag'), findsOneWidget);
      expect(find.bySemanticsLabel('Copy Bitcoin address'), findsOneWidget);
      semantics.dispose();

      final qrs = tester.widgetList<Image>(find.byType(Image)).toList();
      expect(qrs, hasLength(2));
      for (final qr in qrs) {
        expect(qr.filterQuality, FilterQuality.none);
      }
    });
  }

  testWidgets('copy buttons put the exact value on the clipboard',
      (tester) async {
    final copied = <String>[];
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform,
      (call) async {
        if (call.method == 'Clipboard.setData') {
          copied.add((call.arguments as Map)['text'] as String);
        }
        return null;
      },
    );
    addTearDown(() => tester.binding.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, null));

    await _pumpAt(tester, const Size(390, 1800), Brightness.dark);

    for (final tip in [
      'Copy XRP address',
      'Copy XRP destination tag',
      'Copy Bitcoin address',
    ]) {
      await tester.tap(find.byTooltip(tip));
      await tester.pump();
      expect(find.text('Copied'), findsOneWidget);
    }
    expect(copied, [_xrp, _tag, _btc]);
  });

  testWidgets('side by side at 700dp and wider, stacked below',
      (tester) async {
    double dx(String text) => tester.getTopLeft(find.text(text)).dx;
    double dy(String text) => tester.getTopLeft(find.text(text)).dy;

    await _pumpAt(tester, const Size(1280, 1400), Brightness.dark);
    expect(dy('XRP (Ripple)'), dy('Bitcoin (BTC)'));
    expect(dx('Bitcoin (BTC)'), greaterThan(dx('XRP (Ripple)')));

    await _pumpAt(tester, const Size(699, 1800), Brightness.dark);
    expect(dx('XRP (Ripple)'), dx('Bitcoin (BTC)'));
    expect(dy('Bitcoin (BTC)'), greaterThan(dy('XRP (Ripple)')));
  });
}
