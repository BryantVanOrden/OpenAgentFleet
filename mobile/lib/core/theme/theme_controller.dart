import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'theme.dart';

/// Persisted theme choice.
///
/// Mode is tri-state on purpose: "system" is the default, so an operator whose
/// phone flips to dark at sunset gets the same treatment here without touching
/// a setting. Picking light or dark explicitly opts out of that.
@immutable
class ThemeChoice {
  const ThemeChoice(
      {this.mode = ThemeMode.system, this.accent = FleetAccent.amber});

  final ThemeMode mode;
  final FleetAccent accent;

  ThemeChoice copyWith({ThemeMode? mode, FleetAccent? accent}) =>
      ThemeChoice(mode: mode ?? this.mode, accent: accent ?? this.accent);
}

const _modeKey = 'agentfleet.theme.mode';
const _accentKey = 'agentfleet.theme.accent';

class ThemeController extends StateNotifier<ThemeChoice> {
  ThemeController(this._prefs) : super(_read(_prefs));

  final SharedPreferences _prefs;

  static ThemeChoice _read(SharedPreferences prefs) {
    final mode = switch (prefs.getString(_modeKey)) {
      'light' => ThemeMode.light,
      'dark' => ThemeMode.dark,
      _ => ThemeMode.system,
    };
    final accentName = prefs.getString(_accentKey);
    final accent = FleetAccent.values.firstWhere(
      (a) => a.name == accentName,
      orElse: () => FleetAccent.amber,
    );
    return ThemeChoice(mode: mode, accent: accent);
  }

  Future<void> setMode(ThemeMode mode) async {
    state = state.copyWith(mode: mode);
    await _prefs.setString(_modeKey, mode.name);
  }

  Future<void> setAccent(FleetAccent accent) async {
    state = state.copyWith(accent: accent);
    await _prefs.setString(_accentKey, accent.name);
  }
}

/// Overridden in main() once SharedPreferences has loaded.
final sharedPreferencesProvider = Provider<SharedPreferences>(
  (_) => throw UnimplementedError('override in main'),
);

final themeControllerProvider =
    StateNotifierProvider<ThemeController, ThemeChoice>(
  (ref) => ThemeController(ref.watch(sharedPreferencesProvider)),
);
