import 'dart:io';

import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

import '../network/api_client.dart';

/// Push wiring.
///
/// The point of this app is that an agent stuck on a CAPTCHA at 2am can reach
/// you. That means: a channel that survives Do Not Disturb for critical alerts,
/// a foreground handler (Android suppresses the system banner while the app is
/// open), and a tap handler that lands on the alert rather than the home screen.
class PushService {
  PushService(this._api);

  final ApiClient _api;
  final _local = FlutterLocalNotificationsPlugin();

  /// Called when the operator taps a notification. Set by the app shell.
  void Function(String alertId)? onAlertTapped;

  static const _channel = AndroidNotificationChannel(
    'agentfleet_alerts',
    'Agent alerts',
    description: 'An agent needs you, stalled, or finished a task.',
    importance: Importance.max,
  );

  Future<void> init() async {
    try {
      await Firebase.initializeApp();
    } catch (err) {
      // No Firebase config in this build: everything else still works, alerts
      // just arrive when the app is open rather than as a push.
      debugPrint('push: Firebase unavailable ($err); running without push');
      return;
    }

    await _local.initialize(
      const InitializationSettings(
        android: AndroidInitializationSettings('@mipmap/ic_launcher'),
        iOS: DarwinInitializationSettings(
          requestAlertPermission: true,
          requestSoundPermission: true,
        ),
      ),
      onDidReceiveNotificationResponse: (response) {
        final id = response.payload;
        if (id != null && id.isNotEmpty) onAlertTapped?.call(id);
      },
    );

    await _local
        .resolvePlatformSpecificImplementation<AndroidFlutterLocalNotificationsPlugin>()
        ?.createNotificationChannel(_channel);

    final messaging = FirebaseMessaging.instance;
    final settings = await messaging.requestPermission(
      alert: true,
      badge: true,
      sound: true,
      // Lets a "needs human" alert cut through Focus on iOS.
      criticalAlert: false,
    );
    if (settings.authorizationStatus == AuthorizationStatus.denied) {
      debugPrint('push: permission denied');
    }

    // A token can rotate at any time, so register on both paths.
    final token = await messaging.getToken();
    if (token != null) await _register(token);
    messaging.onTokenRefresh.listen(_register);

    FirebaseMessaging.onMessage.listen(_showForeground);
    FirebaseMessaging.onMessageOpenedApp.listen((message) {
      final id = message.data['alert_id'] as String?;
      if (id != null) onAlertTapped?.call(id);
    });

    // Cold start from a notification tap.
    final initial = await messaging.getInitialMessage();
    final initialId = initial?.data['alert_id'] as String?;
    if (initialId != null) onAlertTapped?.call(initialId);
  }

  Future<void> _register(String token) async {
    try {
      await _api.registerDevice(token, Platform.isIOS ? 'ios' : 'android');
    } catch (err) {
      debugPrint('push: could not register device ($err)');
    }
  }

  Future<void> _showForeground(RemoteMessage message) async {
    final notification = message.notification;
    if (notification == null) return;
    final severity = message.data['severity'] as String? ?? 'info';

    await _local.show(
      message.hashCode,
      notification.title,
      notification.body,
      NotificationDetails(
        android: AndroidNotificationDetails(
          _channel.id,
          _channel.name,
          channelDescription: _channel.description,
          importance: severity == 'info' ? Importance.defaultImportance : Importance.max,
          priority: severity == 'info' ? Priority.defaultPriority : Priority.high,
          category: severity == 'critical' ? AndroidNotificationCategory.call : null,
        ),
        iOS: DarwinNotificationDetails(
          interruptionLevel: severity == 'critical'
              ? InterruptionLevel.timeSensitive
              : InterruptionLevel.active,
        ),
      ),
      payload: message.data['alert_id'] as String?,
    );
  }
}

/// Background handler. Must be a top-level function — Flutter spins up a fresh
/// isolate for it, so nothing from the app's state is available here.
@pragma('vm:entry-point')
Future<void> firebaseBackgroundHandler(RemoteMessage message) async {
  await Firebase.initializeApp();
  // Displaying the system notification is handled by the OS from the FCM
  // payload; nothing to do but make sure the isolate boots cleanly.
}
