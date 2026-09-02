import 'package:flutter_test/flutter_test.dart';

import 'package:agentfleet_companion/core/voice/speakable.dart';

// What a TTS voice is given instead of raw chat markdown. Structure goes,
// words stay, digits become words — a small voice model saying "asterisk
// asterisk" or misreading "1.0.1" is the failure these pin against.
void main() {
  group('markdown structure', () {
    test('emphasis markers vanish, words stay', () {
      expect(speakable('**Done** — the *staging* run passed'),
          'Done — the staging run passed');
    });

    test('a fenced block becomes the words "code block"', () {
      final out = speakable('Run this:\n```bash\nmake up && ./deploy.sh\n```\nthen check.');
      expect(out, contains('code block'));
      expect(out, isNot(contains('make up')));
      expect(out, isNot(contains('```')));
    });

    test('inline code keeps its content', () {
      expect(speakable('run `make up` first'), 'run make up first');
    });

    test('links speak their label, bare URLs their host', () {
      expect(speakable('see [the docs](https://example.com/docs/x)'),
          'see the docs');
      expect(speakable('open https://example.com/deep/path?q=1 now'),
          'open example.com now');
    });

    test('headings, quotes, bullets and tables lose their furniture', () {
      final out = speakable(
          '## Result\n> quoted\n- first\n- second\n1. third\n\n| a | b |\n|---|---|\n| c | d |');
      expect(out, isNot(contains('#')));
      expect(out, isNot(contains('>')));
      expect(out, isNot(contains('|')));
      expect(out, contains('Result'));
      expect(out, contains('first'));
      expect(out, contains('c'));
    });

    test('snake_case survives', () {
      expect(speakable('set shell_access to on'), 'set shell_access to on');
    });
  });

  group('numbers', () {
    test('integers become words', () {
      expect(speakable('ran 3 tasks across 21 steps'),
          'ran three tasks across twenty-one steps');
      expect(speakable('1,250 records'),
          'one thousand two hundred fifty records');
    });

    test('decimals read point-by-digit', () {
      expect(speakable('took 3.14 seconds'),
          'took three point one four seconds');
    });

    test('versions read group by group', () {
      expect(speakable('now on v1.0.1'), 'now on vone point zero point one');
      // Groups read as numbers, not digit strings: "two point ten point
      // three" is how a person says 2.10.3 out loud.
      expect(speakable('Airflow 2.10.3'), 'Airflow two point ten point three');
    });

    test('money and percent', () {
      expect(speakable(r'costs $5 or 50% off'),
          'costs five dollars or fifty percent off');
      expect(speakable(r'spent $1.25'),
          'spent one point two five dollars');
    });

    test('clock times', () {
      expect(speakable('at 3:30 then 4:00 then 9:05'),
          "at three thirty then four o'clock then nine oh five");
    });

    test('ranges', () {
      expect(speakable('steps 3-5 failed'), 'steps three to five failed');
    });

    test('huge numbers fall back to digits', () {
      expect(speakable('id 1234567890123456'),
          'id one two three four five six seven eight nine zero one two three four five six');
    });
  });

  test('the kitchen sink', () {
    final out = speakable(
        '**Fixed!** Deployed `v2.1.0` — see [the run](https://ci.example.com/r/9). 3 tests, \$0.02.');
    expect(out,
        'Fixed! Deployed vtwo point one point zero — see the run. three tests, zero point zero two dollars.');
  });
}
