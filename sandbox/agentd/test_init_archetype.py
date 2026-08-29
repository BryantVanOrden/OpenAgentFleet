"""Behaviour of init-archetype.sh, exercised by running the real script.

The functions are lifted verbatim out of the shipped file and sourced into a
bash that has apt and friends stubbed out, so what is under test is the code
that actually runs in the sandbox rather than a paraphrase of it.
"""

import os
import subprocess
import tempfile
import unittest

SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "init-archetype.sh")

# The installer's three functions, in the order they appear. Everything after
# them is top-level work that needs a real container.
FIRST_FUNC = "recipe_for() {"
AFTER_FUNCS = 'install_tools "${TOOLS}"'


def installer_functions():
    with open(SCRIPT, encoding="utf-8") as f:
        body = f.read()
    start = body.index(FIRST_FUNC)
    end = body.index(AFTER_FUNCS)
    return body[start:end]


HARNESS = """
set -euo pipefail

RECIPES="%(recipes)s"
OPERATOR_RECIPES="%(operator)s"

# Nothing may touch the network or the package database.
apt-get() { return 0; }
pip3() { return 0; }
npm() { return 0; }
timeout() { shift; "$@"; }

%(functions)s

install_tools "%(tools)s"
echo "REACHED-END"
"""


def run_installer(tools, recipes="", operator_recipes=""):
    """Run install_tools over `tools` and return (exit status, output)."""
    with tempfile.TemporaryDirectory() as tmp:
        recipes_path = os.path.join(tmp, "tools.conf")
        operator_path = os.path.join(tmp, "custom-tools.conf")
        with open(recipes_path, "w", encoding="utf-8") as f:
            f.write(recipes)
        with open(operator_path, "w", encoding="utf-8") as f:
            f.write(operator_recipes)

        script = HARNESS % {
            "recipes": recipes_path,
            "operator": operator_path,
            "functions": installer_functions(),
            "tools": tools,
        }
        proc = subprocess.run(
            ["bash", "-c", script],
            capture_output=True, text=True, timeout=60,
        )
        return proc.returncode, proc.stdout + proc.stderr


class TestToolInstallation(unittest.TestCase):
    def test_a_tool_with_no_recipe_does_not_abandon_the_rest_of_the_setup(self):
        """tools.conf promises anything unlisted is tried as an apt package of
        its own name. recipe_for returns 1 when it finds nothing, and under
        `set -e` that status aborted the whole initializer -- so the apt
        fallback was unreachable, and .tools-ready, the README and the repo
        clones never happened."""
        code, out = run_installer(
            "zzknown,zznorecipe,zzalsoknown",
            recipes="zzknown=apt:known-pkg\nzzalsoknown=apt:also-pkg\n",
        )
        self.assertIn("REACHED-END", out, f"the installer exited early (status {code}): {out}")
        self.assertEqual(code, 0, out)

    def test_a_tool_with_no_recipe_is_tried_as_an_apt_package(self):
        code, out = run_installer("zznorecipe", recipes="zzknown=apt:known-pkg\n")
        self.assertEqual(code, 0, out)
        self.assertIn("zznorecipe: installed (apt)", out)

    def test_every_listed_tool_is_attempted_even_when_one_has_no_recipe(self):
        # Names chosen not to exist on PATH: a tool already installed is
        # deliberately skipped, which would hide the very thing being checked.
        code, out = run_installer(
            "zzfirst,zznorecipe,zzlast",
            recipes="zzfirst=apt:first-pkg\nzzlast=apt:last-pkg\n",
        )
        self.assertEqual(code, 0, out)
        for tool in ("zzfirst", "zznorecipe", "zzlast"):
            self.assertIn(tool + ":", out, f"{tool} was never attempted: {out}")

    def test_a_recipe_marked_skip_is_reported_not_installed(self):
        code, out = run_installer("zznope", recipes="zznope=skip\n")
        self.assertEqual(code, 0, out)
        self.assertIn("zznope: no unattended install", out)

    def test_an_operator_recipe_overrides_a_built_in_one(self):
        code, out = run_installer(
            "zzthing",
            recipes="zzthing=apt:built-in\n",
            operator_recipes="zzthing=pip:operators-choice\n",
        )
        self.assertEqual(code, 0, out)
        self.assertIn("zzthing: installed (pip)", out)


if __name__ == "__main__":
    unittest.main()
