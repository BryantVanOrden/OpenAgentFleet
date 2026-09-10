"""The accessibility tree is queried in a child process with a hard timeout, so
a wedged AT-SPI registry costs one query and never the daemon."""

import json
import os
import subprocess
import sys

import a11y


def test_node_round_trips_through_json():
    n = a11y.Node(role="frame", name="Fleet Notes", depth=0, x=1, y=2, w=300, h=200,
                  children=[a11y.Node(role="push button", name="Save", depth=1, x=5, y=6, w=40, h=20, focused=True)])
    back = a11y.Node.from_dict(json.loads(json.dumps(n.to_dict())))
    assert back == n
    assert back.children[0].center == (25, 16)


def test_child_answers_with_a_json_list(tmp_path):
    # Without a desktop the child reports an empty tree -- and exits, which is
    # the property that matters: the server never blocks on it.
    script = os.path.join(os.path.dirname(a11y.__file__), "a11y_child.py")
    proc = subprocess.run([sys.executable, script, "tree"], capture_output=True, text=True, timeout=30,
                          cwd=os.path.dirname(script))
    assert proc.returncode == 0, proc.stderr
    assert json.loads(proc.stdout) == []


def test_a_query_that_never_answers_is_dropped_not_waited_for(monkeypatch):
    # Stand in for a wedged registry: a child that sleeps past the timeout.
    monkeypatch.setattr(a11y, "CHILD_TIMEOUT", 0.5)
    monkeypatch.setattr(a11y, "AVAILABLE", True)
    monkeypatch.setattr(a11y, "INPROC", False)

    real_run = subprocess.run

    def slow_run(cmd, **kw):
        raise subprocess.TimeoutExpired(cmd, kw.get("timeout", 0))

    monkeypatch.setattr(subprocess, "run", slow_run)
    try:
        assert a11y.tree() == []
        assert a11y.at_point(1, 1) is None
        assert a11y.window_title_at(1, 1) == ""
        assert a11y.wedged() is True
        assert a11y.status()["wedged"] is True
    finally:
        monkeypatch.setattr(subprocess, "run", real_run)


def test_a_good_answer_clears_the_wedged_flag(monkeypatch):
    monkeypatch.setattr(a11y, "AVAILABLE", True)
    monkeypatch.setattr(a11y, "INPROC", False)
    a11y._wedged_at = 1.0

    class Done:
        returncode = 0
        stdout = json.dumps([a11y.Node(role="window", name="w", depth=0).to_dict()])
        stderr = ""

    monkeypatch.setattr(subprocess, "run", lambda *a, **k: Done())
    roots = a11y.tree()
    assert [r.name for r in roots] == ["w"]
    assert a11y.wedged() is False
