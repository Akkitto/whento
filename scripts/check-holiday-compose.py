#!/usr/bin/env python3
# WhenTo - Collaborative event calendar for self-hosted environments
# Copyright (C) 2025 WhenTo Contributors
# SPDX-License-Identifier: BSL-1.1
"""Verify opt-in holiday egress reaches only the services running the backend."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
SETTING = "HOLIDAYS_NAGER_FALLBACK_ENABLED"


class HolidayComposeTests(unittest.TestCase):
    def test_default_and_explicit_opt_in_and_opt_out(self):
        env = dict(os.environ)
        for name in (SETTING, "COMPOSE_FILE", "COMPOSE_ENV_FILES"):
            env.pop(name, None)
        env.update(
            DB_PASSWORD="compose-test-only", REDIS_PASSWORD="compose-test-only",
            SMTP_HOST="", SMTP_USERNAME="", SMTP_PASSWORD="", SMTP_FROM="",
        )
        with tempfile.TemporaryDirectory(prefix="whento-holiday-compose-") as tmp:
            fixture = Path(tmp) / ".env"
            for value in (None, "false", "true"):
                fixture.write_text("" if value is None else f"{SETTING}={value}\n", encoding="utf-8")
                for compose, backend in (
                    ("docker-compose.yml", "app"),
                    (".devcontainer/docker-compose.yml", "devcontainer"),
                    ("docker-compose.dev.yml", None),
                ):
                    with self.subTest(value=value, compose=compose):
                        result = subprocess.run(
                            ["docker", "compose", "--env-file", str(fixture), "-f", str(ROOT / compose), "config", "--format", "json"],
                            env=env, text=True, capture_output=True, check=True, timeout=30,
                        )
                        services = json.loads(result.stdout)["services"]
                        if backend is not None:
                            self.assertIn(backend, services)
                        for name, service in services.items():
                            settings = service.get("environment", {})
                            if name == backend:
                                self.assertEqual(settings.get(SETTING), value or "false")
                            else:
                                self.assertNotIn(SETTING, settings)


if __name__ == "__main__":
    unittest.main(verbosity=2)
