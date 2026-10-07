#!/usr/bin/env python3
"""Configure disposable hosted Ubuntu only; never change operator machines."""

import os
from pathlib import Path
import re

BOUNDS = '''Acquire::Retries "2";
Acquire::http::Timeout "30";
Acquire::https::Timeout "30";
APT::Update::Error-Mode "any";
'''


def prepare_apt(apt_root):
    config = apt_root / "etc/apt/apt.conf.d"
    mirrors = apt_root / "etc/apt/apt-mirrors.txt"
    bounds = config / "99-whento-pr163-bounds"
    if not config.is_dir() or config.is_symlink():
        raise RuntimeError("Expected the disposable Ubuntu APT configuration directory")
    if mirrors.is_symlink() or bounds.is_symlink():
        raise RuntimeError("Refusing symlinked APT configuration files")
    if mirrors.exists():
        current = mirrors.read_text()
        # The hosted mirror stalled in run 37671235062. Change only its exact
        # URI token; keep suites, priorities, other repositories and signing keys.
        revised = re.sub(r"(?<!\S)https?://azure\.archive\.ubuntu\.com/ubuntu/?(?=\s|$)",
                         "https://archive.ubuntu.com/ubuntu", current)
        if revised != current:
            mirrors.write_text(revised)
    bounds.write_text(BOUNDS)
    bounds.chmod(0o644)


def main():
    if (os.environ.get("GITHUB_ACTIONS") != "true"
            or os.environ.get("GITHUB_REPOSITORY") != "Akkitto/whento"
            or os.geteuid() != 0):
        raise RuntimeError("This setup requires the fork's disposable Actions runner as root")
    prepare_apt(Path("/"))
    print("Official Ubuntu mirror and bounded package acquisition configured.")


if __name__ == "__main__":
    main()
