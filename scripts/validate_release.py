#!/usr/bin/env python3

import re
import sys
import tomllib
from pathlib import Path

VERSION_PATTERN = re.compile(r"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")


def validate(manifest_path: Path, tag: str) -> str:
    with manifest_path.open("rb") as manifest_file:
        manifest = tomllib.load(manifest_file)
    version = manifest.get("version")
    if not isinstance(version, str) or not VERSION_PATTERN.fullmatch(version):
        raise ValueError("herdr-plugin.toml must define a top-level version in strict X.Y.Z form")
    if tag != "v" + version:
        raise ValueError(f"release tag {tag!r} must equal {'v' + version!r}")
    return version


def main() -> int:
    try:
        version = validate(Path(sys.argv[1]), sys.argv[2])
    except (IndexError, OSError, ValueError) as error:
        print(f"release validation failed: {error}", file=sys.stderr)
        return 1
    print(f"release tag {sys.argv[2]} matches plugin version {version}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
