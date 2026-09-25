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
        self.assertEqual(renderer.stability_errors(manifest(), "0.4.0"), [])

    def test_beta_with_a_stable_since_is_refused(self):
        errors = renderer.stability_errors(
            manifest(stability="beta", stableSince="1.0.0"), None)
        self.assertEqual(len(errors), 1)
        self.assertIn("only allowed", errors[0])

    def test_an_unknown_value_is_refused(self):
        errors = renderer.stability_errors(manifest(stability="alpha"), None)
        self.assertEqual(len(errors), 1)

    def test_stable_needs_stable_since(self):
        errors = renderer.stability_errors(manifest(stability="stable"), "1.0.0")
        self.assertIn("stableSince", errors[0])

    def test_stable_since_below_one_is_refused(self):
        for since in ("0.9.0", "0.1.0", "1.0", "1.0.0-rc.1", "v"):
            with self.subTest(since=since):
                errors = renderer.stability_errors(
                    manifest(stability="stable", stableSince=since), None)
                self.assertEqual(len(errors), 1)

    def test_stable_with_a_zero_x_latest_release_is_refused(self):
        errors = renderer.stability_errors(
            manifest(stability="stable", stableSince="1.0.0"), "0.9.3")
        self.assertIn("latest release is 0.9.3", errors[0])

    def test_stable_since_an_unreleased_version_is_refused(self):
        errors = renderer.stability_errors(
            manifest(stability="stable", stableSince="1.2.0"), "1.1.4")
        self.assertIn("newer than the latest release", errors[0])

    def test_stable_is_accepted_once_the_release_exists(self):
        for latest in ("1.0.0", "v1.0.0", "1.3.2", "2.0.0"):
            with self.subTest(latest=latest):
                self.assertEqual(renderer.stability_errors(
                    manifest(stability="stable", stableSince="1.0.0"), latest), [])

    def test_an_unknown_latest_release_skips_the_release_check(self):
        # A shallow clone has no tags; the manifest rules still apply.
        self.assertEqual(renderer.stability_errors(
            manifest(stability="stable", stableSince="1.0.0"), None), [])
        self.assertEqual(renderer.stability_errors(
            manifest(stability="stable", stableSince="1.0.0"), "1.1.0-rc.1"), [])


class BannerTest(unittest.TestCase):
    def test_beta_keeps_the_family_wording(self):
        banner = renderer.render_stability(manifest())
        self.assertTrue(banner.startswith("> **Beta.** The family is days old"))
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
        self.assertIn("**Beta.**", readme)
        self.assertIn("tar -xzf tui-example.tar.gz", readme)

    def test_stable_renders_the_since_line(self):
        result, readme = self.run_script(
            manifest(stability="stable", stableSince="1.0.0"), "1.0.0")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("**Stable since v1.0.0.**", readme)
        self.assertNotIn("**Beta.**", readme)

    def test_a_premature_stable_claim_fails_and_writes_nothing(self):
        result, readme = self.run_script(
            manifest(stability="stable", stableSince="1.0.0"), "0.9.0")
        self.assertEqual(result.returncode, 1)
        self.assertIn("latest release is 0.9.0", result.stderr)
        self.assertEqual(readme, README)

    def test_check_reports_an_out_of_date_banner(self):
        result, _ = self.run_script(manifest(), "0.4.0", check=True)
        self.assertEqual(result.returncode, 1)


if __name__ == "__main__":
    unittest.main()
