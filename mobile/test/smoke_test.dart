import 'package:agentfleet_companion/core/network/api_client.dart';
import 'package:agentfleet_companion/core/state.dart';
import 'package:agentfleet_companion/core/theme/theme.dart';
import 'package:agentfleet_companion/features/auth/login_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  testWidgets('login screen offers a server URL and a sign-in button',
      (tester) async {
    // ApiClient only touches SharedPreferences on construction, so a real one
    // over mocked prefs is a truer fake than a hand-rolled stub.
    SharedPreferences.setMockInitialValues({});
    final api = await ApiClient.create();

    await tester.pumpWidget(
      ProviderScope(
        overrides: [apiProvider.overrideWithValue(api)],
        child: MaterialApp(
          theme: buildTheme(),
          home: const LoginScreen(),
        ),
      ),
    );
    await tester.pump();

    expect(find.text('Orchestrator URL'), findsOneWidget);
    expect(find.widgetWithText(FilledButton, 'Sign in'), findsOneWidget);

    // The URL field is pre-filled with the emulator loopback so a fresh install
    // is one tap from a working local backend.
    final server = tester.widget<TextField>(
      find.ancestor(
        of: find.text('Orchestrator URL'),
        matching: find.byType(TextField),
      ),
    );
    expect(server.controller?.text, 'http://10.0.2.2:8080');
  });
}
