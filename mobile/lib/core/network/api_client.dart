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

  Future<dynamic> _put(String path, [Object? body]) async {
    final res = await _dio.put(path, data: body, options: _auth);
    if (res.statusCode! >= 400) _fail(res);
    return res.data;
  }

  Future<dynamic> _patch(String path, [Object? body]) async {
    final res = await _dio.patch(path, data: body, options: _auth);
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
    String orgId = '',
    /// The archetype's tools, minus any unticked. Null leaves the archetype's
    /// own list alone; an explicit list replaces it.
    List<String>? tools,
    /// Tools the operator added, with how to fetch each one.
    List<CustomTool> customTools = const [],
  }) async {
    final data = await _post('/api/instances', {
      'name': name,
      'tier': tier,
      if (archetypeId != null && archetypeId.isNotEmpty)
        'archetype_id': archetypeId,
      'shell_access': shellAccess,
      if (orgId.isNotEmpty) 'org_id': orgId,
      if (tools != null) 'preinstalled_tools': tools,
      if (customTools.isNotEmpty)
        'custom_tools': [
          for (final c in customTools)
            {'name': c.name, 'method': c.method, 'spec': c.spec},
        ],
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
      _delete('/api/vault/secrets/${Uri.encodeComponent(key)}');

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
    String? conversationId,
  }) async {
    final data = await _post('/api/vault/comms', {
      'content': content,
      'from_instance_name': fromInstanceName,
      'to_instance_id': toInstanceId,
      'kind': kind,
      if (conversationId != null) 'conversation_id': conversationId,
    }) as Map;
    return PeerMessage.fromJson(data.cast<String, dynamic>());
  }

  // ------------------------------------------------- orgs and permissions ---

  Future<List<Org>> orgs() async {
    final data = await _get('/api/orgs') as List? ?? const [];
    return data
        .map((e) => Org.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  Future<Org> saveOrg({String id = '', required String name, String description = ''}) async {
    final body = {'name': name, 'description': description};
    final data = id.isEmpty
        ? await _post('/api/orgs', body) as Map
        : await _put('/api/orgs/$id', body) as Map;
    return Org.fromJson(data.cast<String, dynamic>());
  }

  Future<void> deleteOrg(String id) => _delete('/api/orgs/$id');

  Future<List<OrgMember>> orgMembers(String orgId) async {
    final data = await _get('/api/orgs/$orgId/members') as List? ?? const [];
    return data
        .map((e) => OrgMember.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  Future<void> setOrgMember(String orgId, String userId, String role) =>
      _post('/api/orgs/$orgId/members', {'user_id': userId, 'org_role': role});

  Future<void> removeOrgMember(String orgId, String userId) =>
      _delete('/api/orgs/$orgId/members/$userId');

  Future<List<FleetUser>> users() async {
    final data = await _get('/api/users') as List? ?? const [];
    return data
        .map((e) => FleetUser.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  Future<List<BotGrant>> botGrants(String instanceId) async {
    final data =
        await _get('/api/instances/$instanceId/grants') as List? ?? const [];
    return data
        .map((e) => BotGrant.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  /// Set a per-bot exception. A null [permissions] removes the grant so the
  /// org default applies again; an empty list explicitly allows nothing, which
  /// is how one bot is hidden from someone who can see the rest of their org.
  Future<void> setBotGrant(String instanceId, String userId,
          List<String>? permissions) =>
      _put('/api/instances/$instanceId/grants',
          {'user_id': userId, 'permissions': permissions});

  Future<Instance> setInstanceOrg(String instanceId, String orgId) async {
    final data =
        await _put('/api/instances/$instanceId/org', {'org_id': orgId}) as Map;
    return Instance.fromJson(data.cast<String, dynamic>());
  }

  /// What the signed-in user may do, so the UI can hide what it must rather
  /// than offering actions the server will refuse.
  Future<({bool globalAdmin, Map<String, List<String>> bots})>
      myPermissions() async {
    final data = await _get('/api/me/permissions') as Map? ?? {};
    final bots = <String, List<String>>{};
    ((data['bots'] as Map?) ?? {}).forEach((k, v) {
      bots['$k'] = ((v as List?) ?? const []).map((e) => '$e').toList();
    });
    return (globalAdmin: data['global_admin'] as bool? ?? false, bots: bots);
  }

  // ------------------------------------------------------ model combinations ---

  Future<List<ModelCombo>> modelCombos() async {
    final data = await _get('/api/model-combos') as List? ?? const [];
    return data
        .map((e) => ModelCombo.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  Future<ModelCombo> saveModelCombo(ModelCombo c) async {
    final body = {
      'name': c.name,
      'description': c.description,
      'roles': c.roles,
    };
    final data = c.id.isEmpty
        ? await _post('/api/model-combos', body) as Map
        : await _put('/api/model-combos/${c.id}', body) as Map;
    return ModelCombo.fromJson(data.cast<String, dynamic>());
  }

  Future<void> deleteModelCombo(String id) => _delete('/api/model-combos/$id');

  /// What each role actually resolves to for this bot, after combinations and
  /// per-role fallbacks are applied.
  Future<Map<String, List<String>>> resolvedModels(String instanceId) async {
    final data =
        await _get('/api/instances/$instanceId/models/resolved') as Map? ?? {};
    return data.map((k, v) => MapEntry(
          '$k',
          ((v as List?) ?? const [])
              .map((e) => '${(e as Map)['name'] ?? ''}')
              .where((s) => s.isNotEmpty)
              .toList(),
        ));
  }

  // ------------------------------------------------------------- memories ---

  /// What this bot has chosen to remember.
  Future<List<BotMemory>> instanceMemories(String id) async {
    final data = await _get('/api/instances/$id/memories') as List? ?? const [];
    return data
        .map((e) => BotMemory.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  Future<void> forgetMemory(String instanceId, String memoryId) =>
      _delete('/api/instances/$instanceId/memories/$memoryId');

  // -------------------------------------------------------- conversations ---

  Future<List<Conversation>> conversations() async {
    final data = await _get('/api/comms/conversations') as List? ?? const [];
    return data
        .map((e) => Conversation.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  /// Open a thread. Include [Conversation.operatorId] among the members to be
  /// in it yourself; leave it out to put two bots together and watch.
  Future<Conversation> createConversation({
    required List<String> members,
    String title = '',
  }) async {
    final data = await _post('/api/comms/conversations', {
      'title': title,
      'members': members,
    }) as Map;
    return Conversation.fromJson(data.cast<String, dynamic>());
  }

  Future<void> deleteConversation(String id) =>
      _delete('/api/comms/conversations/$id');

  /// Rename or pin a thread. Independent fields, so pinning keeps the name.
  Future<Conversation> updateConversation(String id,
      {String? title, bool? pinned}) async {
    final data = await _patch('/api/comms/conversations/$id', {
      if (title != null) 'title': title,
      if (pinned != null) 'pinned': pinned,
    }) as Map;
    return Conversation.fromJson(data.cast<String, dynamic>());
  }

  Future<List<PeerMessage>> conversationMessages(String id) async {
    final data =
        await _get('/api/comms/conversations/$id/messages') as List? ?? const [];
    return data
        .map((e) => PeerMessage.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  /// Fold a thread's history into a single summary message. The originals stay
  /// on the server; this changes what is replayed, not what happened.
  Future<PeerMessage> compactConversation(String id) async {
    final data = await _post('/api/comms/conversations/$id/compact') as Map;
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

  Future<List<ChatMessage>> chat(String instanceId, {String? chatId}) async {
    final data = await _get('/api/chat/$instanceId',
        query: chatId == null || chatId.isEmpty ? null : {'chat_id': chatId})
        as List? ?? const [];
    return data
        .map((e) => ChatMessage.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  // -------------------------------------------------------- chats with a bot ---

  Future<List<ChatSession>> chatSessions(String instanceId) async {
    final data =
        await _get('/api/chat/$instanceId/chats') as List? ?? const [];
    return data
        .map((e) => ChatSession.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  Future<ChatSession> createChatSession(String instanceId,
      {String title = ''}) async {
    final data =
        await _post('/api/chat/$instanceId/chats', {'title': title}) as Map;
    return ChatSession.fromJson(data.cast<String, dynamic>());
  }

  /// Rename or pin a chat. Both are optional and independent, so pinning does
  /// not clear the name.
  Future<ChatSession> updateChatSession(String instanceId, String chatId,
      {String? title, bool? pinned}) async {
    final data = await _patch('/api/chat/$instanceId/chats/$chatId', {
      if (title != null) 'title': title,
      if (pinned != null) 'pinned': pinned,
    }) as Map;
    return ChatSession.fromJson(data.cast<String, dynamic>());
  }

  Future<void> deleteChatSession(String instanceId, String chatId) =>
      _delete('/api/chat/$instanceId/chats/$chatId');

  /// Talk to an agent.
  ///
  /// mode is "chat" (talk, no actions), "plan" (propose, still no actions) or
  /// "task" (start work). Chat is the default deliberately: asking how a run is
  /// going must never start one.
  Future<void> sendChat(String instanceId, String body,
          {String mode = 'chat', String? chatId}) =>
      _post('/api/chat/$instanceId', {
        'body': body,
        'mode': mode,
        if (chatId != null && chatId.isNotEmpty) 'chat_id': chatId,
      });

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

  /// Create or update a connection. Passing an existing id edits it.
  ///
  /// [apiKey] is sent only when you are setting or replacing one. The server
  /// seals it into the vault and hands back a reference; the key itself is
  /// never stored on the device and never comes back in a response.
  Future<AIProvider> saveProvider(AIProvider p, {String apiKey = ''}) async {
    final body = {
      'id': p.id,
      'name': p.name,
      'kind': p.kind,
      'model': p.model,
      'base_url': p.baseUrl,
      'vision': p.vision,
      'priority': p.priority,
      'enabled': p.enabled,
      'auth_mode': p.authMode,
      if (p.oauthClientId.isNotEmpty) 'oauth_client_id': p.oauthClientId,
      if (p.apiKeyRef.isNotEmpty) 'api_key_ref': p.apiKeyRef,
      if (apiKey.isNotEmpty) 'api_key': apiKey,
    };
    final data = p.id.isEmpty
        ? await _post('/api/providers', body) as Map
        : await _put('/api/providers/${p.id}', body) as Map;
    return AIProvider.fromJson(data.cast<String, dynamic>());
  }

  Future<void> deleteProvider(String id) => _delete('/api/providers/$id');

  // ------------------------------------------------------ provider sign-in ---

  /// Begin signing in to a provider with a Google account.
  ///
  /// Returns the code to show and the URL to open. The device code stays on
  /// the server; the app only ever handles the short user-facing one.
  Future<({String userCode, String verificationUrl, int interval})>
      startProviderSignIn(
    String id, {
    required String clientId,
    String clientSecret = '',
    String scope = '',
  }) async {
    final data = await _post('/api/providers/$id/signin', {
      'client_id': clientId,
      'client_secret': clientSecret,
      if (scope.isNotEmpty) 'scope': scope,
    }) as Map;
    return (
      userCode: '${data['user_code'] ?? ''}',
      verificationUrl: '${data['verification_url'] ?? ''}',
      interval: (data['interval'] as num?)?.toInt() ?? 5,
    );
  }

  /// The exact redirect URI to register on an OAuth client.
  ///
  /// Asked of the server rather than built here: it has to match what the
  /// server actually sends character for character, and a mismatch is the
  /// commonest way an OAuth setup fails.
  Future<String> oauthRedirectUri() async {
    final data = await _get('/api/providers/oauth/redirect') as Map;
    return '${data['redirect_uri'] ?? ''}';
  }

  /// Begin an in-app sign-in. Returns the consent page to load in a webview
  /// and the redirect the provider will come back to.
  Future<({String authorizeUrl, String state, String redirectUri})>
      startInAppSignIn(
    String id, {
    required String clientId,
    String clientSecret = '',
    String scope = '',
    String authUrl = '',
    String tokenUrl = '',
  }) async {
    final data = await _post('/api/providers/$id/signin/url', {
      'client_id': clientId,
      'client_secret': clientSecret,
      if (scope.isNotEmpty) 'scope': scope,
      if (authUrl.isNotEmpty) 'auth_url': authUrl,
      if (tokenUrl.isNotEmpty) 'token_url': tokenUrl,
    }) as Map;
    return (
      authorizeUrl: '${data['authorize_url'] ?? ''}',
      state: '${data['state'] ?? ''}',
      redirectUri: '${data['redirect_uri'] ?? ''}',
    );
  }

  /// Poll while the webview completes. Returns true once signed in; throws
  /// with the provider's own reason if it failed.
  Future<bool> inAppSignInComplete(String state) async {
    final data = await _get('/api/providers/signin/status',
        query: {'state': state}) as Map;
    return data['status'] == 'signed_in';
  }

  /// Poll while the operator approves. Returns true once signed in.
  Future<bool> providerSignInComplete(String id) async {
    final data = await _get('/api/providers/$id/signin') as Map;
    return data['status'] == 'signed_in';
  }

  Future<void> providerSignOut(String id) =>
      _delete('/api/providers/$id/signin');

  /// Check a connection actually answers. Returns the server's report.
  Future<Map<String, dynamic>> probeProvider(String id) async {
    final data = await _post('/api/providers/$id/probe') as Map;
    return data.cast<String, dynamic>();
  }

  /// Models a connection can actually serve, asked of the engine itself rather
  /// than typed in by hand.
  ///
  /// [live] is false when the server fell back to its built-in catalogue —
  /// worth showing, because a confident list of models the engine may not
  /// serve is how you pick one that 404s three steps into a run.
  Future<({List<String> models, bool live, String reason})> discoverModels({
    required String kind,
    String baseUrl = '',
    String apiKey = '',
  }) async {
    final data = await _get('/api/providers/models', query: {
      'kind': kind,
      if (baseUrl.isNotEmpty) 'base_url': baseUrl,
      if (apiKey.isNotEmpty) 'api_key': apiKey,
    });
    if (data is! Map) return (models: <String>[], live: false, reason: '');
    final list = (data['models'] as List?) ?? const [];
    return (
      models: list.map((e) => _modelName(e)).toList(),
      live: data['live'] as bool? ?? false,
      reason: '${data['reason'] ?? data['error'] ?? ''}',
    );
  }

  static String _modelName(dynamic e) {
    if (e is String) return e;
    if (e is Map) return '${e['name'] ?? e['id'] ?? e['model'] ?? ''}';
    return '$e';
  }

  /// Assign a bot its own ordered model chain. Replaces the list; the order is
  /// the fallback order.
  Future<Instance> setInstanceModels(
      String instanceId, List<String> providerIds) async {
    final data = await _put('/api/instances/$instanceId/models',
        {'provider_ids': providerIds}) as Map;
    return Instance.fromJson(data.cast<String, dynamic>());
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
