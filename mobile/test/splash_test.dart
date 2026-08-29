import 'package:agentfleet_companion/features/splash/splash_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('splash shows the mark without needing a provider scope',
      (tester) async {
    // The point of this test: the splash is rendered before ProviderScope and
    // before FleetThemeSync exist, so it must not reach for either. Pumping it
    // bare is exactly the situation it ships into.
    await tester.pumpWidget(const MaterialApp(home: SplashScreen()));

    expect(find.text('AF'), findsOneWidget);
    expect(find.text('AgentFleet'), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
  });

  testWidgets('splash can carry a custom message', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(home: SplashScreen(message: 'Reconnecting...')),
    );
    expect(find.text('Reconnecting...'), findsOneWidget);
  });

  testWidgets('start-up failure is reported with a retry', (tester) async {
    var retried = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: SplashError(
          error: 'prefs unavailable',
          onRetry: () => retried++,
        ),
      ),
    );

    expect(find.text('AgentFleet could not start'), findsOneWidget);
    expect(find.text('prefs unavailable'), findsOneWidget);

    await tester.tap(find.text('Try again'));
    expect(retried, 1);
  });
}
