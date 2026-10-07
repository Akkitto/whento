#!/usr/bin/env python3
# WhenTo - Collaborative event calendar for self-hosted environments
# Copyright (C) 2025 WhenTo Contributors
# SPDX-License-Identifier: BSL-1.1
"""Check bootstrap interpolation without starting containers or reading .env."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent


class BootstrapComposeTests(unittest.TestCase):
    def test_bootstrap_settings_reach_only_backend_services(self):
        cases = (
            ("unset", "", ""),
            ("pinned", "compose-test-only-boot-key-0123456789abcdef", ""),
            ("file", "", "/run/secrets/bootstrap_key"),
        )
        # Override all interpolated credentials with synthetic values. Bootstrap
        # settings must come from the fixture .env, not inherited shell values.
        env = dict(os.environ)
        env.update(
            DB_USER="whento", DB_NAME="whento", DB_PASSWORD="compose-test-only",
            REDIS_PASSWORD="compose-test-only", APP_ENV="development",
            APP_URL="http://localhost:8080", SMTP_HOST="", SMTP_USERNAME="",
            SMTP_PASSWORD="", SMTP_FROM="",
        )
        for name in ("BOOTSTRAP_KEY", "BOOTSTRAP_KEY_FILE", "COMPOSE_FILE", "COMPOSE_ENV_FILES"):
            env.pop(name, None)
        with tempfile.TemporaryDirectory(prefix="whento-compose-bootstrap-") as tmp:
            fixture = Path(tmp) / ".env"
            for label, key, secret_file in cases:
                fixture.write_text(
                    f"BOOTSTRAP_KEY={key}\nBOOTSTRAP_KEY_FILE={secret_file}\n",
                    encoding="utf-8",
                )
                for compose, backend in (
                    ("docker-compose.yml", "app"),
                    (".devcontainer/docker-compose.yml", "devcontainer"),
                    ("docker-compose.dev.yml", None),
                ):
                    with self.subTest(case=label, compose=compose):
                        result = subprocess.run(
                            ["docker", "compose", "--env-file", str(fixture),
                             "-f", str(ROOT / compose), "config", "--format", "json"],
                            cwd=ROOT, env=env, capture_output=True, text=True,
                            timeout=30, check=True,
                        )
                        services = json.loads(result.stdout)["services"]
                        if backend is not None:
                            settings = services[backend]["environment"]
                            self.assertEqual(settings.get("BOOTSTRAP_KEY"), key)
                            self.assertEqual(settings.get("BOOTSTRAP_KEY_FILE"), secret_file)
                        else:
                            self.assertEqual(set(services), {"postgres", "redis"})
                        for name, service in services.items():
                            if name != backend:
                                settings = service.get("environment", {})
                                self.assertNotIn("BOOTSTRAP_KEY", settings)
                                self.assertNotIn("BOOTSTRAP_KEY_FILE", settings)


if __name__ == "__main__":
    unittest.main(verbosity=2)
