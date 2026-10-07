#!/usr/bin/env python3
# WhenTo - Collaborative event calendar for self-hosted environments
# Copyright (C) 2025 WhenTo Contributors
# SPDX-License-Identifier: BSL-1.1
"""Check reminder settings reach the backend without starting containers."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
DEFAULTS = {
    "REMINDER_HOURS_BEFORE": "24",
    "REMINDER_INTERVAL": "5m",
    "REMINDER_CATCH_UP_WINDOW": "15m",
    "REMINDER_MAX_ATTEMPTS": "5",
    "REMINDER_RETRY_BACKOFF": "1m",
}


class ReminderComposeTests(unittest.TestCase):
    def test_defaults_and_overrides_reach_only_backend_services(self):
        overrides = dict(zip(DEFAULTS, ("48", "10m", "30m", "8", "2m")))
        env = dict(os.environ)
        for name in (*DEFAULTS, "COMPOSE_FILE", "COMPOSE_ENV_FILES"):
            env.pop(name, None)
        env.update(
            DB_PASSWORD="compose-test-only", REDIS_PASSWORD="compose-test-only",
            SMTP_HOST="", SMTP_USERNAME="", SMTP_PASSWORD="", SMTP_FROM="",
        )
        with tempfile.TemporaryDirectory(prefix="whento-reminder-compose-") as tmp:
            fixture = Path(tmp) / ".env"
            for settings in ({}, overrides):
                fixture.write_text("".join(f"{key}={value}\n" for key, value in settings.items()), encoding="utf-8")
                for compose, backend in (
                    ("docker-compose.yml", "app"),
                    (".devcontainer/docker-compose.yml", "devcontainer"),
                    ("docker-compose.dev.yml", None),
                ):
                    with self.subTest(settings=settings, compose=compose):
                        result = subprocess.run(
                            ["docker", "compose", "--env-file", str(fixture), "-f", str(ROOT / compose), "config", "--format", "json"],
                            env=env, text=True, capture_output=True, check=True, timeout=30,
                        )
                        services = json.loads(result.stdout)["services"]
                        for name, service in services.items():
                            actual = service.get("environment", {})
                            for key, default in DEFAULTS.items():
                                if name == backend:
                                    self.assertEqual(actual.get(key), settings.get(key, default))
                                else:
                                    self.assertNotIn(key, actual)


if __name__ == "__main__":
    unittest.main(verbosity=2)
