import 'package:dio/dio.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../models.dart';

/// One client for the orchestrator. Holds the base URL and the session token,
/// both of which survive a restart — an operator woken by an alert at 03:00
/// should not have to sign in before they can see what is wrong.
class ApiClient {
  ApiClient._(this._dio, this._prefs);

  static const _tokenKey = 'agentfleet.token';
  static const _baseKey = 'agentfleet.base_url';

  final Dio _dio;
  final SharedPreferences _prefs;

  static Future<ApiClient> create() async {
    final prefs = await SharedPreferences.getInstance();
    final dio = Dio(
      BaseOptions(
        connectTimeout: const Duration(seconds: 15),
        receiveTimeout: const Duration(minutes: 3),
        headers: {'Content-Type': 'application/json'},
        // Non-2xx is handled explicitly below, not by throwing opaque errors.
        validateStatus: (code) => code != null && code < 500,
      ),
    );
    final client = ApiClient._(dio, prefs);
    client._applyBaseUrl(prefs.getString(_baseKey) ?? '');
    return client;
  }

  String get baseUrl => _prefs.getString(_baseKey) ?? '';
  String? get token => _prefs.getString(_tokenKey);
  bool get isConfigured => baseUrl.isNotEmpty;
  bool get isAuthenticated => (token ?? '').isNotEmpty;

  void _applyBaseUrl(String url) => _dio.options.baseUrl = url;

  Future<void> setBaseUrl(String url) async {
    final trimmed = url.trim().replaceAll(RegExp(r'/+$'), '');
    await _prefs.setString(_baseKey, trimmed);
    _applyBaseUrl(trimmed);
  }

  Future<void> _setToken(String? value) async {
    if (value == null) {
      await _prefs.remove(_tokenKey);
    } else {
      await _prefs.setString(_tokenKey, value);
    }
  }

  Options get _auth => Options(headers: {'Authorization': 'Bearer $token'});

  Never _fail(Response res) {
    final data = res.data;
    final message = data is Map && data['error'] is String
        ? data['error'] as String
        : 'Request failed (${res.statusCode})';
    throw ApiException(message, res.statusCode ?? 0);
  }

  Future<dynamic> _get(String path, {Map<String, dynamic>? query}) async {
    final res = await _dio.get(path, queryParameters: query, options: _auth);
    if (res.statusCode! >= 400) _fail(res);
    return res.data;
  }

  Future<dynamic> _post(String path, [Object? body]) async {
    final res = await _dio.post(path, data: body, options: _auth);
    if (res.statusCode! >= 400) _fail(res);
    return res.data;
  }

  // ------------------------------------------------------------------ auth ---

  Future<void> login(String email, String password) async {
    final res = await _dio.post('/api/auth/login', data: {
      'email': email,
      'password': password,
    });
    if (res.statusCode! >= 400) _fail(res);
    await _setToken(res.data['token'] as String);
  }

  Future<void> logout() => _setToken(null);

  // ----------------------------------------------------------------- fleet ---

  Future<List<Instance>> instances() async {
    final data = await _get('/api/instances') as List;
    return data.map((e) => Instance.fromJson((e as Map).cast<String, dynamic>())).toList();
  }

  Future<Instance> instance(String id) async =>
      Instance.fromJson((await _get('/api/instances/$id') as Map).cast<String, dynamic>());

  Future<InstanceStats> stats(String id) async =>
      InstanceStats.fromJson((await _get('/api/instances/$id/stats') as Map).cast<String, dynamic>());

  Future<void> instanceAction(String id, String action) => _post('/api/instances/$id/$action');

  /// Single frame instead of a live stream — the right default on mobile data.
  Future<String?> observe(String id) async {
    final data = await _get('/api/instances/$id/observe', query: {'a11y': false}) as Map;
    return data['screenshot_b64'] as String?;
  }

  // ----------------------------------------------------------------- tasks ---

  Future<List<Task>> tasks({String? instanceId}) async {
    final data = await _get('/api/tasks',
        query: instanceId == null ? null : {'instance_id': instanceId}) as List;
    return data.map((e) => Task.fromJson((e as Map).cast<String, dynamic>())).toList();
  }

  Future<Task> createTask({
    required String instanceId,
    required String goal,
    String? skillId,
  }) async {
    final data = await _post('/api/tasks', {
      'instance_id': instanceId,
      'goal': goal,
      if (skillId != null && skillId.isNotEmpty) 'skill_id': skillId,
    }) as Map;
    return Task.fromJson(data.cast<String, dynamic>());
  }

  Future<void> cancelTask(String id) => _post('/api/tasks/$id/cancel');

  Future<List<Skill>> skills() async {
    final data = await _get('/api/skills') as List;
    return data.map((e) => Skill.fromJson((e as Map).cast<String, dynamic>())).toList();
  }

  // ---------------------------------------------------------------- alerts ---

  Future<List<Alert>> alerts({bool openOnly = false}) async {
    final data = await _get('/api/alerts', query: {'open': openOnly}) as List;
    return data.map((e) => Alert.fromJson((e as Map).cast<String, dynamic>())).toList();
  }

  Future<void> replyAlert(String id, String reply) =>
      _post('/api/alerts/$id/reply', {'reply': reply});

  // ------------------------------------------------------------------ chat ---

  Future<List<ChatMessage>> chat(String instanceId) async {
    final data = await _get('/api/chat/$instanceId') as List;
    return data.map((e) => ChatMessage.fromJson((e as Map).cast<String, dynamic>())).toList();
  }

  Future<void> sendChat(String instanceId, String body, {bool asTask = false}) =>
      _post('/api/chat/$instanceId', {'body': body, 'as_task': asTask});

  // --------------------------------------------------------------- swarms ---

  Future<List<SwarmTeam>> swarms() async {
    final data = await _get('/api/swarms') as List;
    return data.map((e) => SwarmTeam.fromJson((e as Map).cast<String, dynamic>())).toList();
  }

  Future<SwarmTeam> swarm(String id) async {
    final data = await _get('/api/swarms/$id') as Map<String, dynamic>;
    return SwarmTeam.fromJson(data);
  }

  Future<SwarmTeam> createSwarm(String name, String mission) async {
    final data = await _post('/api/swarms', {'name': name, 'mission': mission})
        as Map<String, dynamic>;
    return SwarmTeam.fromJson(data);
  }

  Future<SwarmMessage> postSwarmMessage(
    String id,
    String content, {
    String fromBot = 'Mobile Operator',
    String toBot = 'all',
    String phase = 'execution',
  }) async {
    final data = await _post('/api/swarms/$id/messages', {
      'from_bot': fromBot,
      'to_bot': toBot,
      'phase': phase,
      'content': content,
    }) as Map<String, dynamic>;
    return SwarmMessage.fromJson(data);
  }

  // --------------------------------------------------------------- devices ---

  Future<void> registerDevice(String pushToken, String platform) =>
      _post('/api/devices', {'token': pushToken, 'platform': platform});

  // ------------------------------------------------------------------- urls ---

  /// noVNC through the orchestrator's authenticated proxy. The token has to ride
  /// in `path` too, because that is what noVNC uses to build its socket URL.
  String desktopUrl(String instanceId) {
    final encoded = Uri.encodeComponent(token ?? '');
    final query = {
      'autoconnect': 'true',
      'resize': 'scale',
      'reconnect': 'true',
      'path': 'vnc/$instanceId/websockify?token=$encoded',
      'token': token ?? '',
    };
    return Uri.parse('$baseUrl/vnc/$instanceId/vnc.html')
        .replace(queryParameters: query)
        .toString();
  }

  String artifactUrl(String key) =>
      '$baseUrl/api/artifacts/$key?token=${Uri.encodeComponent(token ?? '')}';

  Uri eventsUri({String? instanceId}) {
    final base = Uri.parse(baseUrl);
    return base.replace(
      scheme: base.scheme == 'https' ? 'wss' : 'ws',
      path: '/api/events',
      queryParameters: {
        'token': token ?? '',
        if (instanceId != null) 'instance_id': instanceId,
      },
    );
  }
}

class ApiException implements Exception {
  ApiException(this.message, this.status);
  final String message;
  final int status;

  @override
  String toString() => message;
}
