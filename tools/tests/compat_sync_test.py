#!/usr/bin/env python3
"""Tests for tools/compat-sync.py: evidence versions are read through the
backend's versionRegex before they go into `tested` (tui-kit#19)."""
from __future__ import annotations

import contextlib
import importlib.util
import io
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest

TOOLS = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SCRIPT = os.path.join(TOOLS, "compat-sync.py")


def load():
    spec = importlib.util.spec_from_file_location("compat_sync", SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


sync = load()

# The versionRegex tui-dc ships for `samba-tool --version`, whose Ubuntu build
# prints "4.19.5-Ubuntu": the probe reports 4.19.5.
SAMBA_RE = r"(?m)^([0-9]+\.[0-9]+(?:\.[0-9]+)?)\S*\s*$"


def result(version, distro="ubuntu-24.04", outcome="pass", backend="samba"):
    return {"tool": "tui-dc", "backend": backend, "version": version,
            "distro": distro, "date": "2026-09-12", "result": outcome,
            "suite": "smoke"}


class CanonicalVersionTest(unittest.TestCase):

    def test_the_regex_reads_the_recorded_string(self):
        self.assertEqual(sync.canonical_version("4.19.5-Ubuntu", SAMBA_RE),
                         "4.19.5")
        self.assertEqual(sync.canonical_version("4.24.6", SAMBA_RE), "4.24.6")

    def test_a_regex_anchored_on_context_uses_its_group(self):
        # The family's manifests anchor on the text around the version,
        # which a recorded bare version does not carry.
        for version, pattern, want in (
                ("0.36.2", "ufw ([0-9][0-9.]*)", "0.36.2"),
                ("9.6p1", r"OpenSSH_([0-9]+\.[0-9]+)", "9.6"),
                ("257.2-1-arch", "systemd ([0-9]+)", "257"),
                ("4.19.5", r"Version ([0-9]+(\.[0-9]+){0,2})", "4.19.5"),
                ("1.2.3", r"v(?P<v>\d+\.\d+\.\d+)", "1.2.3"),
                ("2.11.0", r"certbot (\d+\.\d+(?:\.\d+)?)", "2.11.0"),
        ):
            self.assertEqual(sync.canonical_version(version, pattern), want,
                             pattern)

    def test_no_regex_takes_the_default_token_like_the_probe(self):
        # compat.ParseVersion keeps the suffix when there is no regex, so
        # tested keeps it too: both sides agree either way.
        self.assertEqual(sync.canonical_version("4.19.5-Ubuntu", ""),
                         "4.19.5-Ubuntu")
        self.assertEqual(sync.canonical_version("v0.36.2", ""), "0.36.2")

    def test_nothing_matching_keeps_the_recorded_string(self):
        self.assertEqual(sync.canonical_version("garbage", "ufw ([0-9]+)"),
                         "garbage")

    def test_a_broken_regex_falls_back_to_the_default_token(self):
        self.assertEqual(sync.canonical_version("4.19.5-Ubuntu", "(["),
                         "4.19.5-Ubuntu")

    def test_first_group(self):
        self.assertEqual(sync.first_group(r"(?:a)(?i)x(\d+(\.\d+)?)"),
                         r"\d+(\.\d+)?")
        self.assertEqual(sync.first_group(r"[(]\((\d+)"), r"\d+")
        self.assertEqual(sync.first_group(r"(?P<v>\d+)"), r"\d+")
        self.assertIsNone(sync.first_group(r"\d+(?:\.\d+)"))


class TestedVersionsTest(unittest.TestCase):

    def test_evidence_stays_raw_and_tested_is_canonical(self):
        results = [result("4.24.6", "fedora-44"),
                   result("4.19.5-Ubuntu"),
                   result("4.19.5", "debian-12"),
                   result("4.24.7", "omarchy-server-4.0.1"),
                   result("4.20.0", outcome="fail")]
        self.assertEqual(
            sync.tested_versions(results, "tui-dc", "samba", SAMBA_RE),
            ["4.19.5", "4.24.6", "4.24.7"])

    def test_an_unusable_version_is_reported(self):
        results = [result("4.19.5"), result("not-a-version", "arch")]
        err = io.StringIO()
        with contextlib.redirect_stderr(err):
            got = sync.tested_versions(results, "tui-dc", "samba", SAMBA_RE)
        self.assertEqual(got, ["4.19.5"])
        self.assertIn("'not-a-version'", err.getvalue())
        self.assertIn("arch", err.getvalue())


class ScriptTest(unittest.TestCase):
    """The script end to end on a temporary manifest."""

    def test_sync_then_check(self):
        with tempfile.TemporaryDirectory() as tmp:
            manifest = pathlib.Path(tmp, "tool.json")
            results = pathlib.Path(tmp, "results.jsonl")
            manifest.write_text(json.dumps({"name": "tui-dc", "backends": [
                {"name": "samba", "binary": "samba-tool",
                 "versionCommand": ["samba-tool", "--version"],
                 "versionRegex": SAMBA_RE}]}))
            results.write_text("\n".join(json.dumps(r) for r in (
                result("4.19.5-Ubuntu"), result("4.24.6", "fedora-44"))) + "\n")
            run = [sys.executable, SCRIPT, "--manifest", str(manifest),
                   "--results", str(results)]
            check = subprocess.run(run + ["--check"], capture_output=True,
                                   text=True, timeout=60)
            self.assertEqual(check.returncode, 1, check.stdout + check.stderr)
            subprocess.run(run, check=True, capture_output=True, timeout=60)
            tested = json.loads(manifest.read_text())["backends"][0]["tested"]
            self.assertEqual(tested, ["4.19.5", "4.24.6"])
            # The evidence is left as recorded.
            self.assertIn("4.19.5-Ubuntu", results.read_text())
            check = subprocess.run(run + ["--check"], capture_output=True,
                                   text=True, timeout=60)
            self.assertEqual(check.returncode, 0, check.stdout + check.stderr)


if __name__ == "__main__":
    unittest.main()
