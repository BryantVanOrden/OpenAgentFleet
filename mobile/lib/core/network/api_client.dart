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

  Future<dynamic> _delete(String path) async {
    final res = await _dio.delete(path, options: _auth);
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
    // `as List? ?? const []` rather than a bare `as List` throughout: an
    // endpoint that yields a JSON null -- a nil slice on the Go side
    // serialises that way -- would otherwise throw "Null is not a subtype
    // of List<dynamic>" and take the whole screen down instead of
    // rendering an empty one.
    final data = await _get('/api/instances') as List? ?? const [];
    return data
        .map((e) => Instance.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  Future<Instance> instance(String id) async => Instance.fromJson(
      (await _get('/api/instances/$id') as Map).cast<String, dynamic>());

  Future<InstanceStats> stats(String id) async => InstanceStats.fromJson(
      (await _get('/api/instances/$id/stats') as Map).cast<String, dynamic>());

  Future<void> instanceAction(String id, String action) =>
      _post('/api/instances/$id/$action');

  Future<List<TierProfile>> tiers() async {
    final data = await _get('/api/tiers') as List? ?? const [];
    return data
        .map((e) => TierProfile.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  Future<List<BotTemplate>> templates() async {
    final data = await _get('/api/templates') as List? ?? const [];
    return data
        .map((e) => BotTemplate.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  /// Provision a sandbox. `tier` must be one of [tiers]; the orchestrator
  /// rejects an unknown name rather than quietly substituting a smaller box.
  Future<Instance> createInstance({
    required String name,
    required String tier,
    String? archetypeId,
    bool shellAccess = false,
  }) async {
    final data = await _post('/api/instances', {
      'name': name,
      'tier': tier,
      if (archetypeId != null && archetypeId.isNotEmpty)
        'archetype_id': archetypeId,
      'shell_access': shellAccess,
    }) as Map;
    return Instance.fromJson(data.cast<String, dynamic>());
  }

  Future<void> deleteInstance(String id) => _delete('/api/instances/$id');

  // ----------------------------------------------------------------- voice ---

  /// Voices offered by the server's speech service.
  ///
  /// Returns an empty list when no service is deployed — that is a normal
  /// configuration, not an error, and the caller falls back to the device's
  /// own synthesiser.
  Future<List<ServerVoice>> serverVoices() async {
    final data = await _get('/api/voice/voices') as Map?;
    if (data == null || data['available'] != true) return const [];
    return ((data['voices'] as List?) ?? const [])
        .map((e) => ServerVoice.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  /// Synthesise speech. Returns WAV bytes.
  Future<List<int>> speak(String text, {String? voice, double speed = 1.0}) async {
    final res = await _dio.post<List<int>>(
      '/api/voice/speak',
      data: {'text': text, if (voice != null) 'voice': voice, 'speed': speed},
      options: Options(
        responseType: ResponseType.bytes,
        headers: _auth.headers,
        validateStatus: (_) => true,
      ),
    );
    if (res.statusCode! >= 400) {
      throw ApiException('speech synthesis failed', res.statusCode ?? 0);
    }
    return res.data ?? const [];
  }

  // ------------------------------------------------------------------ host ---

  /// Live usage of the machine running the orchestrator.
  Future<HostStats> hostStats() async => HostStats.fromJson(
      (await _get('/api/telemetry/host') as Map).cast<String, dynamic>());

  /// Single frame instead of a live stream — the right default on mobile data.
  Future<String?> observe(String id) async {
    final data =
        await _get('/api/instances/$id/observe', query: {'a11y': false}) as Map;
    return data['screenshot_b64'] as String?;
  }

  // ----------------------------------------------------------------- tasks ---

  Future<List<Task>> tasks({String? instanceId}) async {
    final data = await _get('/api/tasks',
        query: instanceId == null ? null : {'instance_id': instanceId}) as List? ?? const [];
    return data
        .map((e) => Task.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
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
    final data = await _get('/api/skills') as List? ?? const [];
    return data
        .map((e) => Skill.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  Future<void> startRecording(String instanceId, String name) =>
      _post('/api/instances/$instanceId/record/start', {'name': name});

  Future<Skill> stopRecording(String instanceId) async {
    final data = await _post('/api/instances/$instanceId/record/stop') as Map;
    return Skill.fromJson(data.cast<String, dynamic>());
  }

  // -------------------------------------------------------- vault & comms ---

  Future<List<SharedSecret>> sharedSecrets() async {
    final data = await _get('/api/vault/secrets') as List? ?? const [];
    return data.map((e) => SharedSecret.fromJson((e as Map).cast<String, dynamic>())).toList();
  }

  Future<SharedSecret> putSharedSecret({
    required String key,
    required String value,
    String scope = 'fleet',
    String note = '',
  }) async {
    final data = await _post('/api/vault/secrets', {
      'key': key,
      'value': value,
      'scope': scope,
      'note': note,
    }) as Map;
    return SharedSecret.fromJson(data.cast<String, dynamic>());
  }

  Future<void> deleteSharedSecret(String key) =>
      _dio.delete('/api/vault/secrets/${Uri.encodeComponent(key)}');

  Future<List<SharedSession>> sharedSessions({String? domain}) async {
    final data = await _get('/api/vault/sessions',
        query: domain == null ? null : {'domain': domain}) as List? ?? const [];
    return data.map((e) => SharedSession.fromJson((e as Map).cast<String, dynamic>())).toList();
  }

  Future<List<PeerMessage>> peerMessages({String? instanceId}) async {
    final data = await _get('/api/vault/comms',
        query: instanceId == null ? null : {'instance_id': instanceId}) as List? ?? const [];
    return data.map((e) => PeerMessage.fromJson((e as Map).cast<String, dynamic>())).toList();
  }

  Future<PeerMessage> sendPeerMessage({
    required String content,
    String fromInstanceName = 'Mobile Operator',
    String toInstanceId = 'broadcast',
    String kind = 'message',
  }) async {
    final data = await _post('/api/vault/comms', {
      'content': content,
      'from_instance_name': fromInstanceName,
      'to_instance_id': toInstanceId,
      'kind': kind,
    }) as Map;
    return PeerMessage.fromJson(data.cast<String, dynamic>());
  }

  // ------------------------------------------------------------ pipelines ---

  Future<List<WorkflowPipeline>> pipelines() async {
    final data = await _get('/api/pipelines') as List? ?? const [];
    return data.map((e) => WorkflowPipeline.fromJson((e as Map).cast<String, dynamic>())).toList();
  }

  Future<PipelineRun> runPipeline(String id) async {
    final data = await _post('/api/pipelines/$id/run') as Map;
    return PipelineRun.fromJson(data.cast<String, dynamic>());
  }

  // ---------------------------------------------------------------- alerts ---

  Future<List<Alert>> alerts({bool openOnly = false}) async {
    final data = await _get('/api/alerts', query: {'open': openOnly}) as List? ?? const [];
    return data
        .map((e) => Alert.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  Future<void> replyAlert(String id, String reply) =>
      _post('/api/alerts/$id/reply', {'reply': reply});

  // ------------------------------------------------------------------ chat ---

  Future<List<ChatMessage>> chat(String instanceId) async {
    final data = await _get('/api/chat/$instanceId') as List? ?? const [];
    return data
        .map((e) => ChatMessage.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  /// Talk to an agent.
  ///
  /// mode is "chat" (talk, no actions), "plan" (propose, still no actions) or
  /// "task" (start work). Chat is the default deliberately: asking how a run is
  /// going must never start one.
  Future<void> sendChat(String instanceId, String body,
          {String mode = 'chat'}) =>
      _post('/api/chat/$instanceId', {'body': body, 'mode': mode});

  /// Turn a proposed plan into a running task.
  Future<void> approvePlan(String instanceId, String planId) =>
      _post('/api/chat/$instanceId/plans/$planId/approve');

  Future<void> discardPlan(String instanceId, String planId) =>
      _post('/api/chat/$instanceId/plans/$planId/discard');

  /// Set the voice a particular agent speaks in. Empty returns it to the
  /// app-wide default.
  Future<Instance> setInstanceVoice(String instanceId, String voice) async {
    final res = await _dio.put('/api/instances/$instanceId/access',
        data: {'voice': voice}, options: _auth);
    if (res.statusCode! >= 400) _fail(res);
    return Instance.fromJson((res.data as Map).cast<String, dynamic>());
  }

  /// Grant or revoke sudo inside a running agent's sandbox. Immediate.
  Future<Instance> setSudoAccess(String instanceId, bool allowed) async {
    final res = await _dio.put('/api/instances/$instanceId/access',
        data: {'sudo_access': allowed}, options: _auth);
    if (res.statusCode! >= 400) _fail(res);
    return Instance.fromJson((res.data as Map).cast<String, dynamic>());
  }

  /// Turn shell access on or off for a running agent. Takes effect on its next
  /// step. Sudo is not settable here — it is fixed when the instance is built.
  Future<Instance> setShellAccess(String instanceId, bool allowed) async {
    final res = await _dio.put('/api/instances/$instanceId/access',
        data: {'shell_access': allowed}, options: _auth);
    if (res.statusCode! >= 400) _fail(res);
    return Instance.fromJson((res.data as Map).cast<String, dynamic>());
  }

  // ------------------------------------------------------------- schedules ---

  /// Scheduled wakeups: the fleet starting work on its own.
  Future<List<CronTrigger>> cronTriggers() async {
    final data = await _get('/api/triggers/cron') as List? ?? const [];
    return data
        .map((e) => CronTrigger.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  Future<List<WebhookTrigger>> webhookTriggers() async {
    final data = await _get('/api/webhooks') as List? ?? const [];
    return data
        .map((e) => WebhookTrigger.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  // --------------------------------------------------------------- swarms ---

  Future<List<SwarmTeam>> swarms() async {
    final data = await _get('/api/swarms') as List? ?? const [];
    return data
        .map((e) => SwarmTeam.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
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

  // ------------------------------------------------------------- providers ---

  Future<List<AIProvider>> providers() async {
    final data = await _get('/api/providers') as List? ?? const [];
    return data
        .map((e) => AIProvider.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  Future<List<AIProvider>> reorderProviders(List<String> ids) async {
    final data = await _post('/api/providers/reorder', {'ids': ids}) as List? ?? const [];
    return data
        .map((e) => AIProvider.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
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
      // 'scale', not 'remote'. remote asks the VNC server to resize its
      // framebuffer to the client, and this one cannot: x11vnc runs without
      // xrandr support over a fixed-size Xvfb, so the request is ignored and
      // the canvas stays at native resolution — on a phone you get a corner of
      // the desktop and taps land nowhere near your finger. scale fits the
      // frame to the view and noVNC translates pointer coordinates itself.
      'resize': 'scale',
      'reconnect': 'true',
      // A visible cursor. On a touch screen there is no hover, so without this
      // there is no way to tell where the pointer actually is.
      'show_dot': 'true',
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
