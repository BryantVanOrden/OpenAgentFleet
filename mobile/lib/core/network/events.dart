import 'dart:async';
import 'dart:convert';

import 'package:web_socket_channel/web_socket_channel.dart';

import '../models.dart';
import 'api_client.dart';

/// Reconnecting event stream.
///
/// On a phone the socket dies constantly — screen off, network handover, app
/// backgrounded. This wraps that reality rather than pretending a WebSocket is
/// durable: callers get one long-lived broadcast stream and never see the churn.
class EventStream {
  EventStream(this._api, {this.instanceId});

  final ApiClient _api;
  final String? instanceId;

  final _controller = StreamController<FleetEvent>.broadcast();
  final _connection = StreamController<bool>.broadcast();

  WebSocketChannel? _channel;
  StreamSubscription<dynamic>? _subscription;
  Timer? _retry;
  int _attempt = 0;
  bool _disposed = false;

  Stream<FleetEvent> get events => _controller.stream;
  Stream<bool> get connected => _connection.stream;

  void start() {
    if (_disposed || !_api.isAuthenticated) return;
    _connect();
  }

  void _connect() {
    try {
      final channel = WebSocketChannel.connect(_api.eventsUri(instanceId: instanceId));
      _channel = channel;
      _subscription = channel.stream.listen(
        (frame) {
          _attempt = 0;
          _connection.add(true);
          try {
            final decoded = jsonDecode(frame as String) as Map<String, dynamic>;
            _controller.add(FleetEvent.fromJson(decoded));
          } catch (_) {
            // A malformed frame is not worth tearing the stream down for.
          }
        },
        onDone: _scheduleReconnect,
        onError: (_) => _scheduleReconnect(),
        cancelOnError: true,
      );
    } catch (_) {
      _scheduleReconnect();
    }
  }

  void _scheduleReconnect() {
    _connection.add(false);
    _subscription?.cancel();
    _subscription = null;
    _channel = null;
    if (_disposed) return;

    _attempt = (_attempt + 1).clamp(1, 6);
    final delay = Duration(milliseconds: (500 * (1 << _attempt)).clamp(500, 20000));
    _retry?.cancel();
    _retry = Timer(delay, _connect);
  }

  Future<void> dispose() async {
    _disposed = true;
    _retry?.cancel();
    await _subscription?.cancel();
    await _channel?.sink.close();
    await _controller.close();
    await _connection.close();
  }
}
