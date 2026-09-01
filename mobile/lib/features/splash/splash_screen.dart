import 'package:flutter/material.dart';

/// The branded first frame.
///
/// On Android and iOS the OS draws its own splash from the generated assets and
/// this only covers the handover; on desktop there is no native splash at all,
/// so this is the only thing standing between launch and the first real screen.
class SplashScreen extends StatelessWidget {
  const SplashScreen({super.key, this.message});

  /// Shown under the mark. Left null for a plain splash; set it when the wait
  /// has a cause worth naming, such as a failed start.
  final String? message;

  @override
  Widget build(BuildContext context) {
    // Deliberately not read from the theme: this can be on screen before the
    // provider scope exists, so it has to stand on its own.
    const ink = Color(0xFF0D0F14);
    const amber = Color(0xFFF5A524);

    return Scaffold(
      backgroundColor: ink,
      body: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Image.asset(
              'assets/branding/mascot.png',
              width: 110,
              height: 110,
            ),
            const SizedBox(height: 24),
            const Text(
              'OpenAgentFleet',
              style: TextStyle(
                color: Colors.white,
                fontSize: 26,
                fontWeight: FontWeight.w700,
              ),
            ),
            const SizedBox(height: 8),
            Text(
              message ?? 'Watch, talk to, and take over your agents.',
              textAlign: TextAlign.center,
              style: TextStyle(
                color: Colors.white.withValues(alpha: 0.55),
                fontSize: 14,
              ),
            ),
            const SizedBox(height: 32),
            const SizedBox(
              width: 22,
              height: 22,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                valueColor: AlwaysStoppedAnimation(amber),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Shown when start-up fails outright. Without this the app dies to a blank
/// window on desktop and a bare red error screen in debug, neither of which
/// says what went wrong.
class SplashError extends StatelessWidget {
  const SplashError({super.key, required this.error, required this.onRetry});

  final Object error;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    const ink = Color(0xFF0D0F14);
    return Scaffold(
      backgroundColor: ink,
      body: Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              // Literal, not Fleet.bad: the theme statics are populated by
              // FleetThemeSync, which is not mounted this early.
              const Icon(Icons.error_outline,
                  color: Color(0xFFF87171), size: 40),
              const SizedBox(height: 16),
              const Text(
                'OpenAgentFleet could not start',
                style: TextStyle(
                  color: Colors.white,
                  fontSize: 18,
                  fontWeight: FontWeight.w700,
                ),
              ),
              const SizedBox(height: 8),
              Text(
                '$error',
                textAlign: TextAlign.center,
                style: TextStyle(
                  color: Colors.white.withValues(alpha: 0.6),
                  fontSize: 13,
                ),
              ),
              const SizedBox(height: 24),
              FilledButton(onPressed: onRetry, child: const Text('Try again')),
            ],
          ),
        ),
      ),
    );
  }
}
