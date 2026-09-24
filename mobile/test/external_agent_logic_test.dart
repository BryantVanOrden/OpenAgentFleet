import 'package:agentfleet_companion/core/models.dart';
import 'package:agentfleet_companion/features/dashboard/external_agent_logic.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('underAnyRoot', () {
    test('the root itself and anything inside it', () {
      expect(underAnyRoot('C:/work', ['C:/work']), isTrue);
      expect(underAnyRoot('C:/work/app', ['C:/work']), isTrue);
      expect(underAnyRoot('c:\\Work\\App\\', ['C:/work/']), isTrue);
      expect(underAnyRoot('/home/me/projects/x', ['/home/me/projects']), isTrue);
    });

    test('a sibling that merely shares a prefix is outside', () {
      expect(underAnyRoot('C:/workshop', ['C:/work']), isFalse);
      expect(underAnyRoot('/etc', ['/home/me']), isFalse);
      expect(underAnyRoot('', ['/home/me']), isFalse);
      expect(underAnyRoot('/home/me', const []), isFalse);
    });
  });

  group('folderUnder', () {
    test('blank is the root', () {
      expect(folderUnder('C:/work', ''), 'C:/work');
      expect(folderUnder('C:/work', '   '), 'C:/work');
    });

    test('joins with the root own separator', () {
      expect(folderUnder('C:/work', 'app'), 'C:/work/app');
      expect(folderUnder('C:/work/', '/app/src'), 'C:/work/app/src');
      expect(folderUnder('C:\\work', 'app/src'), 'C:\\work\\app\\src');
      expect(folderUnder('/home/me', 'a\\b'), '/home/me/a/b');
    });

    test('refuses to climb out', () {
      expect(folderUnder('C:/work', '../secrets'), isNull);
      expect(folderUnder('C:/work', 'a/../../b'), isNull);
    });

    test('what it builds is inside the root', () {
      for (final sub in ['', 'a', 'a/b', '/x']) {
        final f = folderUnder('D:/code', sub)!;
        expect(underAnyRoot(f, ['D:/code']), isTrue, reason: f);
      }
    });
  });

  group('checkAgentUrl', () {
    test('OpenClaw wants a websocket address', () {
      expect(checkAgentUrl(AgentKind.openClaw, 'wss://gw.example:18789'), isNull);
      expect(checkAgentUrl(AgentKind.openClaw, 'ws://10.0.0.2:18789'), isNull);
      expect(checkAgentUrl(AgentKind.openClaw, 'https://gw.example'),
          contains('ws://'));
    });

    test('a webhook wants http or https', () {
      expect(checkAgentUrl(AgentKind.webhook, 'https://x.example/hook'), isNull);
      expect(checkAgentUrl(AgentKind.webhook, 'ftp://x.example'),
          contains('http'));
    });

    test('blank or hostless is refused', () {
      expect(checkAgentUrl(AgentKind.webhook, ''), isNotNull);
      expect(checkAgentUrl(AgentKind.webhook, 'not a url'), isNotNull);
    });
  });

  test('devicesFor leaves out phones and PCs without the CLI', () {
    const pc = OafDevice(id: 'pc', name: 'PC', kind: 'pc', runtimes: ['codex']);
    const old = OafDevice(id: 'old', name: 'Old', kind: 'pc');
    const phone = OafDevice(id: 'ph', name: 'Phone', kind: 'phone');
    final list = [pc, old, phone];
    expect(devicesFor(AgentKind.codex, list).map((d) => d.id), ['pc', 'old']);
    expect(devicesFor(AgentKind.claudeCode, list).map((d) => d.id), ['old']);
  });
}
