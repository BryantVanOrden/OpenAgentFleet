import 'dart:convert';
import 'dart:io';

import 'package:agentfleet_companion/core/network/api_client.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Records what the client actually put on the wire.
class _Recorded {
  _Recorded(this.method, this.path, this.authorization);
  final String method;
  final String path;
  final String? authorization;
}

/// A real HTTP server, so what is under test is the request the client sends
/// rather than a restatement of the code that builds it.
class _FakeBackend {
  _FakeBackend(this._server, this.received);
  final HttpServer _server;
  final List<_Recorded> received;

  static Future<_FakeBackend> start({int secretsStatus = 204}) async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final received = <_Recorded>[];
    server.listen((req) async {
      received.add(_Recorded(
        req.method,
        req.uri.path,
        req.headers.value(HttpHeaders.authorizationHeader),
      ));
      if (req.uri.path == '/api/auth/login') {
        req.response
          ..statusCode = 200
          ..headers.contentType = ContentType.json
          ..write(jsonEncode({'token': 'test-token', 'expires_at': ''}));
      } else {
        req.response.statusCode = secretsStatus;
        if (secretsStatus >= 400) {
          req.response
            ..headers.contentType = ContentType.json
            ..write(jsonEncode({'error': 'authentication required'}));
        }
      }
      await req.response.close();
    });
    return _FakeBackend(server, received);
  }

  String get url => 'http://127.0.0.1:${_server.port}';
  Future<void> stop() => _server.close(force: true);
}

Future<ApiClient> _signedInClient(_FakeBackend backend) async {
  SharedPreferences.setMockInitialValues({});
  final api = await ApiClient.create();
  await api.setBaseUrl(backend.url);
  await api.login('someone@test.local', 'password');
  backend.received.clear();
  return api;
}

void main() {
  // Every other call routes through the private helpers, which attach the
  // bearer token. This one called dio directly, so it went out unauthenticated
  // to a route gated on the admin role.
  test('deleting a shared secret carries the caller\'s credentials', () async {
    final backend = await _FakeBackend.start();
    addTearDown(backend.stop);
    final api = await _signedInClient(backend);

    await api.deleteSharedSecret('deploy-key');

    expect(backend.received, hasLength(1));
    expect(backend.received.single.method, 'DELETE');
    expect(backend.received.single.path, '/api/vault/secrets/deploy-key');
    expect(
      backend.received.single.authorization,
      'Bearer test-token',
      reason: 'the request went out unauthenticated, so the server rejects it',
    );
  });

  // validateStatus admits anything below 500 without throwing, so a handler
  // that does not inspect the status reports a rejected request as done. The
  // vault screen then reloaded and drew the secret it had just "deleted".
  test('a rejected secret deletion is reported to the caller', () async {
    final backend = await _FakeBackend.start(secretsStatus: 401);
    addTearDown(backend.stop);
    final api = await _signedInClient(backend);

    await expectLater(
      api.deleteSharedSecret('deploy-key'),
      throwsA(isA<ApiException>().having((e) => e.status, 'status', 401)),
    );
  });

  test('a successful secret deletion completes quietly', () async {
    final backend = await _FakeBackend.start();
    addTearDown(backend.stop);
    final api = await _signedInClient(backend);

    await expectLater(api.deleteSharedSecret('deploy-key'), completes);
  });
}
