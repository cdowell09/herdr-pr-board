#!/usr/bin/env python3

import json
import sys
import tomllib
from pathlib import Path

GOOS = {"linux": "linux", "macos": "darwin", "windows": "windows"}


def build_matrix(path: Path) -> dict:
    with path.open("rb") as manifest:
        platforms = tomllib.load(manifest).get("platforms")
    if not isinstance(platforms, list) or not platforms:
        raise ValueError("manifest must contain a nonempty platforms array")
    targets = []
    for platform in platforms:
        if not isinstance(platform, str) or platform not in GOOS:
            raise ValueError(f"unsupported manifest platform {platform!r}")
        targets.append({"platform": platform, "goos": GOOS[platform]})
    return {"include": targets}


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("usage: ci_platform_matrix.py path/to/herdr-plugin.toml", file=sys.stderr)
        raise SystemExit(2)
    try:
        print(json.dumps(build_matrix(Path(sys.argv[1])), separators=(",", ":")))
    except (OSError, ValueError) as error:
        print(f"ci-platform-matrix: {error}", file=sys.stderr)
        raise SystemExit(1)
