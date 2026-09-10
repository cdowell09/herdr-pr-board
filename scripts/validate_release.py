#!/usr/bin/env python3

import re
import sys
import tomllib
from pathlib import Path

VERSION_PATTERN = re.compile(r"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")
CONSTANT_PATTERN = re.compile(r'^const Current = "([^"]*)"$', re.MULTILINE)
GO_VERSION_FILE = ("internal", "version", "version.go")
GO_VERSION_NAME = "/".join(GO_VERSION_FILE)


def read_go_version(source_path: Path) -> str:
    try:
        source = source_path.read_text(encoding="utf-8")
    except OSError as error:
        raise ValueError(f"{GO_VERSION_NAME} is not readable: {error}") from error
    match = CONSTANT_PATTERN.search(source)
    if match is None:
        raise ValueError(f'{GO_VERSION_NAME} must define const Current = "X.Y.Z"')
    return match.group(1)


def validate(manifest_path: Path, tag: str) -> str:
    with manifest_path.open("rb") as manifest_file:
        manifest = tomllib.load(manifest_file)
    version = manifest.get("version")
    if not isinstance(version, str) or not VERSION_PATTERN.fullmatch(version):
        raise ValueError("herdr-plugin.toml must define a top-level version in strict X.Y.Z form")
    if tag != "v" + version:
        raise ValueError(f"release tag {tag!r} must equal {'v' + version!r}")
    constant = read_go_version(manifest_path.parent.joinpath(*GO_VERSION_FILE))
    if constant != version:
        raise ValueError(f"{GO_VERSION_NAME} defines {constant!r} but herdr-plugin.toml defines {version!r}")
    return version


def main() -> int:
    try:
        version = validate(Path(sys.argv[1]), sys.argv[2])
    except (IndexError, OSError, ValueError) as error:
        print(f"release validation failed: {error}", file=sys.stderr)
        return 1
    print(f"release tag {sys.argv[2]} matches plugin version {version} and {GO_VERSION_NAME}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
