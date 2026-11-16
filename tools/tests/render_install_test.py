#!/usr/bin/env python3
"""Tests for the stability banner and its checks in tools/render-install.py.

Run with `make tools-test` or `python3 -m unittest discover -s tools/tests`.
"""
from __future__ import annotations

import importlib.util
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest

TOOLS = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SCRIPT = os.path.join(TOOLS, "render-install.py")


def load_renderer():
    """Import render-install.py, whose name is not a Python identifier."""
    spec = importlib.util.spec_from_file_location("render_install", SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


renderer = load_renderer()


def manifest(**extra) -> dict:
    """The smallest manifest the renderer needs, plus whatever a test adds."""
    base = {
        "name": "tui-example",
        "install": {
            "binary": {"available": True, "command": "tar -xzf tui-example.tar.gz"},
        },
    }
    base.update(extra)
    return base


README = """# tui-example

<!-- stability:start -->
old banner
<!-- stability:end -->

## Install

<!-- install:start -->
<!-- install:end -->
"""


class StabilityErrorsTest(unittest.TestCase):
    def test_a_manifest_without_the_field_is_beta_and_valid(self):
        self.assertEqual(renderer.stability_errors(manifest()), [])

    def test_beta_with_a_stable_since_is_refused(self):
        errors = renderer.stability_errors(manifest(stability="beta", stableSince="1.0.0"))
        self.assertEqual(len(errors), 1)
        self.assertIn("only allowed", errors[0])

    def test_an_unknown_value_is_refused(self):
        self.assertEqual(len(renderer.stability_errors(manifest(stability="alpha"))), 1)

    def test_stable_needs_stable_since(self):
        errors = renderer.stability_errors(manifest(stability="stable"))
        self.assertIn("stableSince", errors[0])

    def test_stable_since_below_one_is_refused(self):
        for since in ("0.9.0", "0.1.0", "1.0", "1.0.0-rc.1", "v"):
            with self.subTest(since=since):
                errors = renderer.stability_errors(
                    manifest(stability="stable", stableSince=since))
                self.assertEqual(len(errors), 1)

    def test_stable_since_one_or_later_is_accepted(self):
        for since in ("1.0.0", "1.4.2", "2.0.0"):
            with self.subTest(since=since):
                self.assertEqual(renderer.stability_errors(
                    manifest(stability="stable", stableSince=since)), [])


class StabilityWarningsTest(unittest.TestCase):
    stable = manifest(stability="stable", stableSince="1.0.0")

    def test_a_release_below_stable_since_is_a_pending_promotion(self):
        for latest in ("0.9.3", "v0.9.3"):
            with self.subTest(latest=latest):
                warnings = renderer.stability_warnings(self.stable, latest)
                self.assertEqual(len(warnings), 1)
                self.assertIn("stable since v1.0.0, pending that tag", warnings[0])
        warnings = renderer.stability_warnings(
            manifest(stability="stable", stableSince="1.2.0"), "1.1.4")
        self.assertEqual(len(warnings), 1)

    def test_no_warning_once_the_release_exists(self):
        for latest in ("1.0.0", "v1.0.0", "1.3.2", "2.0.0"):
            with self.subTest(latest=latest):
                self.assertEqual(renderer.stability_warnings(self.stable, latest), [])

    def test_no_warning_without_a_known_release_or_for_beta(self):
        self.assertEqual(renderer.stability_warnings(self.stable, None), [])
        self.assertEqual(renderer.stability_warnings(self.stable, "1.1.0-rc.1"), [])
        self.assertEqual(renderer.stability_warnings(manifest(), "0.1.0"), [])


class BannerTest(unittest.TestCase):
    def test_beta_keeps_the_family_wording(self):
        banner = renderer.render_stability(manifest())
        self.assertTrue(banner.startswith("> **Beta** tools may still change keys"))
        self.assertEqual(banner, renderer.render_stability(manifest(stability="beta")))

    def test_stable_names_the_release_and_links_the_bar(self):
        banner = renderer.render_stability(
            manifest(stability="stable", stableSince="1.0.0"))
        self.assertTrue(banner.startswith("> **Stable since v1.0.0.**"))
        self.assertIn("docs/stability.md", banner)
        self.assertNotIn("Beta", banner)
        for line in banner.split("\n"):
            self.assertTrue(line.startswith("> "), line)

    def test_splice_replaces_only_between_the_markers(self):
        out = renderer.splice_stability(README, "NEW", required=False)
        self.assertIn("<!-- stability:start -->\nNEW\n<!-- stability:end -->", out)
        self.assertNotIn("old banner", out)
        self.assertTrue(out.startswith("# tui-example\n"))

    def test_a_beta_readme_without_markers_is_left_alone(self):
        plain = "# tui-example\n\n> **Beta.** hand-written\n"
        self.assertEqual(renderer.splice_stability(plain, "NEW", required=False), plain)

    def test_a_stable_readme_without_markers_is_refused(self):
        with self.assertRaises(SystemExit):
            renderer.splice_stability("# tui-example\n", "NEW", required=True)


class CommandLineTest(unittest.TestCase):
    """The script end to end, the way a tool's `make readme` calls it."""

    def run_script(self, data: dict, version: str, check: bool = False):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            (root / "tool.json").write_text(json.dumps(data))
            (root / "README.md").write_text(README)
            argv = [sys.executable, SCRIPT, "--manifest", str(root / "tool.json"),
                    "--readme", str(root / "README.md"), "--version", version]
            if check:
                argv.append("--check")
            result = subprocess.run(argv, capture_output=True, text=True)
            return result, (root / "README.md").read_text()

    def test_beta_renders_the_banner(self):
        result, readme = self.run_script(manifest(), "0.4.0")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("**Beta** tools", readme)
        self.assertIn("tar -xzf tui-example.tar.gz", readme)

    def test_stable_renders_the_since_line(self):
        result, readme = self.run_script(
            manifest(stability="stable", stableSince="1.0.0"), "1.0.0")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("**Stable since v1.0.0.**", readme)
        self.assertNotIn("**Beta** tools", readme)

    def test_a_pending_promotion_renders_and_warns(self):
        # The promotion PR: stable is set before the v1.0.0 tag exists.
        result, readme = self.run_script(
            manifest(stability="stable", stableSince="1.0.0"), "0.9.0")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("pending that tag", result.stderr)
        self.assertIn("**Stable since v1.0.0.**", readme)

    def test_check_passes_on_a_pending_promotion_once_rendered(self):
        data = manifest(stability="stable", stableSince="1.0.0")
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            (root / "tool.json").write_text(json.dumps(data))
            (root / "README.md").write_text(README)
            base = [sys.executable, SCRIPT, "--manifest", str(root / "tool.json"),
                    "--readme", str(root / "README.md")]
            subprocess.run(base + ["--version", "1.0.0"], check=True,
                           capture_output=True)
            result = subprocess.run(base + ["--version", "0.9.0", "--check"],
                                    capture_output=True, text=True)
        # The install section expands {version} differently, but this
        # manifest has no placeholder, so only the stability logic decides.
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("pending that tag", result.stderr)

    def test_an_invalid_claim_fails_and_writes_nothing(self):
        result, readme = self.run_script(
            manifest(stability="stable", stableSince="0.9.0"), "0.9.0")
        self.assertEqual(result.returncode, 1)
        self.assertIn("1.0.0-or-later", result.stderr)
        self.assertEqual(readme, README)

    def test_check_reports_an_out_of_date_banner(self):
        result, _ = self.run_script(manifest(), "0.4.0", check=True)
        self.assertEqual(result.returncode, 1)


REPO = "https://pkgs.tui.tools"
PINNED = renderer.KEY_FINGERPRINT
OTHER = "AAAABBBBCCCCDDDDEEEEFFFF0000111122223333"


def repo_manifest() -> dict:
    """A manifest with every repository channel, so every manual setup renders."""
    channel = {"available": True, "requires_repo_setup": True}
    return {
        "name": "tui-example",
        "install": {
            "repo_url": REPO,
            "apt": dict(channel, command="sudo apt install tui-example"),
            "dnf": dict(channel, command="sudo dnf install tui-example"),
            "pacman": dict(channel, command="sudo pacman -S tui-example"),
        },
    }


def colons(*fingerprints: str) -> str:
    """What `gpg --show-keys --with-colons` prints for these primary keys,
    each with a subkey whose fingerprint must not be taken for a primary."""
    out = []
    for fpr in fingerprints:
        out += [
            "pub:-:255:22:0A1B2C3D4E5F6071:1756512000:::-:::scESC:::::ed25519:::0:",
            f"fpr:::::::::{fpr}:",
            "uid:-::::1756512000::X::tui-tools package signing::::::::::0:",
            "sub:-:255:18:1122334455667788:1756512000::::::e:::::cv25519::",
            f"fpr:::::::::{OTHER}:",
        ]
    return "\n".join(out) + "\n"


class ManualSetupTest(unittest.TestCase):
    """The by-hand repository setup checks the key the way install.sh does."""

    def test_every_manual_setup_checks_the_pinned_key_first(self):
        body = renderer.render(repo_manifest(), "0.4.0", "amd64")
        for key in ("apt", "dnf", "pacman"):
            manual = renderer.MANUAL_SETUP[key].format(repo=REPO, fpr=PINNED)
            self.assertIn(manual, body)
            lines = manual.split("\n")
            check = next(i for i, l in enumerate(lines) if "echo \"key OK\"" in l)
            trusts = [
                i for i, l in enumerate(lines)
                if "--dearmor" in l or "rpm --import" in l or "pacman-key" in l
            ]
            self.assertTrue(trusts, key)
            self.assertLess(check, min(trusts), f"{key} trusts the key before checking it")

    def test_dnf_writes_the_repo_file_naming_the_checked_key(self):
        manual = renderer.MANUAL_SETUP["dnf"].format(repo=REPO, fpr=PINNED)
        self.assertNotIn("tui-tools.repo " + REPO, manual)
        self.assertNotIn(REPO + "/rpm/tui-tools.repo", manual)
        self.assertIn("'gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-tui-tools'", manual)
        self.assertIn("'repo_gpgcheck=1'", manual)
        # The reader answers dnf's question; the manual never answers it for them.
        self.assertNotIn(" -y", manual)

    def test_pacman_signs_the_pinned_fingerprint_not_one_read_from_the_file(self):
        manual = renderer.MANUAL_SETUP["pacman"].format(repo=REPO, fpr=PINNED)
        self.assertIn(f"sudo pacman-key --lsign-key {PINNED}", manual)

    def test_a_fingerprint_override_reaches_the_readme(self):
        body = renderer.render(repo_manifest(), "0.4.0", "amd64", OTHER)
        self.assertIn(f"= {OTHER} ]", body)
        self.assertNotIn(PINNED, body)

    def run_check(self, served: str) -> tuple[str, bool]:
        """Run the key check for real, with curl and gpg stubbed: curl
        "downloads" the colon listing, gpg prints the file it is shown."""
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            stubs = root / "bin"
            stubs.mkdir()
            served_file = root / "served"
            served_file.write_text(served)
            (stubs / "curl").write_text(
                '#!/bin/sh\nwhile [ "$1" != -o ]; do shift; done\n'
                f'cp "{served_file}" "$2"\n'
            )
            (stubs / "gpg").write_text('#!/bin/sh\nfor a; do f="$a"; done\ncat "$f"\n')
            for stub in stubs.iterdir():
                stub.chmod(0o755)
            script = renderer.KEY_CHECK.format(repo=REPO, fpr=PINNED)
            script += '\n[ -e "$k" ] && echo "kept" || echo "removed"\n'
            env = dict(os.environ, PATH=f"{stubs}:{os.environ.get('PATH', '/usr/bin:/bin')}",
                       TMPDIR=str(root))
            result = subprocess.run(["sh", "-c", script], capture_output=True,
                                    text=True, env=env, check=True)
            return result.stdout, "kept" in result.stdout

    def test_the_check_accepts_the_pinned_key_alone(self):
        out, kept = self.run_check(colons(PINNED))
        self.assertIn("key OK", out)
        self.assertTrue(kept)

    def test_the_check_refuses_another_key_and_removes_it(self):
        out, kept = self.run_check(colons(OTHER))
        self.assertIn("WRONG KEY", out)
        self.assertFalse(kept)

    def test_the_check_refuses_the_pinned_key_with_another(self):
        for served in (colons(PINNED, OTHER), colons(OTHER, PINNED)):
            out, kept = self.run_check(served)
            self.assertIn("WRONG KEY", out)
            self.assertFalse(kept)

    def test_the_check_refuses_a_file_that_is_no_key(self):
        out, kept = self.run_check("gpg: no valid OpenPGP data found\n")
        self.assertIn("WRONG KEY", out)
        self.assertFalse(kept)


if __name__ == "__main__":
    unittest.main()
