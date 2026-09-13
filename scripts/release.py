#!/usr/bin/env python3
"""Build deterministic Braidplane CLI release archives."""

import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile
import zipfile


SEMVER_RE = re.compile(
    r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(?:-((?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)"
    r"(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?\Z"
)
HASH_RE = re.compile(r"[0-9a-f]{40}(?:[0-9a-f]{24})?\Z")
TARGETS = tuple(
    (goos, goarch)
    for goos in ("darwin", "linux", "windows")
    for goarch in ("amd64", "arm64")
)
LICENSE_NAMES = ("LICENSE", "COPYING", "NOTICE")


class ReleaseError(Exception):
    pass


def git(source: Path, *args: str, input_data: bytes | None = None) -> bytes:
    return subprocess.run(
        ["git", "-C", str(source), *args],
        input=input_data,
        capture_output=True,
        check=True,
    ).stdout


def verify_checkout(source: Path, tag: str, expected_tree: str) -> None:
    head = git(source, "rev-parse", "--verify", "HEAD").decode().strip()
    tagged = git(source, "rev-parse", "--verify", f"refs/tags/{tag}^{{commit}}").decode().strip()
    if tagged != head:
        raise ReleaseError(f"tag {tag} does not resolve to HEAD")
    if git(source, "status", "--porcelain=v1", "-z", "--untracked-files=all"):
        raise ReleaseError("source checkout has tracked or untracked changes")

    records = [record for record in git(source, "ls-tree", "-z", "HEAD").split(b"\0") if record]
    provenance = []
    retained = []
    for record in records:
        header, path = record.split(b"\t", 1)
        if path == b"canonical-source.json":
            provenance.append(header.split())
        else:
            retained.append(record)
    if len(provenance) != 1 or provenance[0][:2] != [b"100644", b"blob"]:
        raise ReleaseError("canonical-source.json must be one regular tracked file")
    reconstructed = git(source, "mktree", "-z", input_data=b"\0".join(retained) + b"\0").decode().strip()
    if reconstructed != expected_tree:
        raise ReleaseError("public source does not match canonical cli_tree")


def validate_source(source: Path, tag: str) -> None:
    if not SEMVER_RE.fullmatch(tag):
        raise ReleaseError("--tag must be a strict vX.Y.Z SemVer tag")
    provenance_path = source / "canonical-source.json"
    try:
        provenance = json.loads(provenance_path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ReleaseError("canonical-source.json is missing or invalid") from exc
    if not isinstance(provenance, dict) or set(provenance) != {"canonical_commit", "cli_tree", "release_version"}:
        raise ReleaseError("canonical-source.json must contain release provenance")
    if provenance["release_version"] != tag:
        raise ReleaseError("canonical-source.json release_version does not match --tag")
    if not all(
        isinstance(provenance[key], str) and HASH_RE.fullmatch(provenance[key])
        for key in ("canonical_commit", "cli_tree")
    ):
        raise ReleaseError("canonical-source.json contains an invalid Git object ID")
    verify_checkout(source, tag, provenance["cli_tree"])
    for name in ("LICENSE", "README.md"):
        if not (source / name).is_file():
            raise ReleaseError(f"required archive file is missing: {name}")


def go_json(source: Path, env: dict[str, str]) -> list[dict]:
    output = subprocess.run(
        ["go", "list", "-deps", "-json", "./cmd/braidplane"],
        cwd=source,
        env=env,
        check=True,
        capture_output=True,
        text=True,
    ).stdout
    decoder = json.JSONDecoder()
    values = []
    position = 0
    while position < len(output):
        while position < len(output) and output[position].isspace():
            position += 1
        if position == len(output):
            break
        value, position = decoder.raw_decode(output, position)
        values.append(value)
    return values


def license_files(directory: Path) -> list[Path]:
    result = []
    for path in directory.iterdir():
        name = path.name.upper()
        if path.is_file() and any(
            name == base or name.startswith((base + ".", base + "-", base + "_"))
            for base in LICENSE_NAMES
        ):
            result.append(path)
    return sorted(result, key=lambda path: path.name)


def third_party_notices(source: Path, env: dict[str, str]) -> bytes:
    goroot = Path(
        subprocess.run(
            ["go", "env", "GOROOT"],
            cwd=source,
            env=env,
            check=True,
            capture_output=True,
            text=True,
        ).stdout.strip()
    )
    go_license = goroot / "LICENSE"
    if not go_license.is_file():
        go_license = goroot.parent / "LICENSE"
    if not go_license.is_file():
        raise ReleaseError("Go standard library LICENSE is missing")

    modules = {}
    for package in go_json(source, env):
        module = package.get("Module")
        if not module or module.get("Main"):
            continue
        effective = module.get("Replace") or module
        path = effective.get("Path")
        version = effective.get("Version")
        directory = effective.get("Dir")
        if not all(isinstance(value, str) and value for value in (path, version, directory)):
            raise ReleaseError("dependency module identity is incomplete")
        modules[(path, version)] = Path(directory)

    sections = [
        b"Braidplane CLI third-party notices\n\n",
        b"Go standard library\n===================\n\n",
        go_license.read_bytes().rstrip() + b"\n",
    ]
    for (path, version), directory in sorted(modules.items()):
        licenses = license_files(directory)
        if not licenses:
            raise ReleaseError(f"dependency has no LICENSE, COPYING, or NOTICE file: {path} {version}")
        heading = f"\n{path} {version}\n{'=' * (len(path) + len(version) + 1)}\n".encode()
        sections.append(heading)
        for license_path in licenses:
            sections.append(f"\n--- {license_path.name} ---\n".encode())
            sections.append(license_path.read_bytes().rstrip() + b"\n")
    return b"".join(sections)


def add_tar_file(archive: tarfile.TarFile, name: str, data: bytes, mode: int) -> None:
    info = tarfile.TarInfo(name)
    info.size = len(data)
    info.mode = mode
    info.mtime = 0
    info.uid = info.gid = 0
    info.uname = info.gname = ""
    archive.addfile(info, io.BytesIO(data))


def tar_archive(path: Path, files: list[tuple[str, bytes, int]]) -> None:
    with path.open("wb") as output:
        with gzip.GzipFile(filename="", mode="wb", fileobj=output, mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.GNU_FORMAT) as archive:
                for name, data, mode in files:
                    add_tar_file(archive, name, data, mode)


def zip_archive(path: Path, files: list[tuple[str, bytes, int]]) -> None:
    with zipfile.ZipFile(path, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for name, data, mode in files:
            info = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.create_system = 3
            info.external_attr = mode << 16
            archive.writestr(info, data)


def build(source: Path, dist: Path, tag: str) -> None:
    validate_source(source, tag)
    dist.mkdir(parents=False, exist_ok=False)
    version = tag.removeprefix("v")
    shared = [
        (name, (source / name).read_bytes(), 0o644)
        for name in ("LICENSE", "README.md", "canonical-source.json")
    ]
    archives = []
    with tempfile.TemporaryDirectory() as temporary:
        temporary_path = Path(temporary)
        for goos, goarch in TARGETS:
            binary_name = "braidplane.exe" if goos == "windows" else "braidplane"
            binary = temporary_path / f"{goos}-{goarch}-{binary_name}"
            env = os.environ.copy()
            env.update(GOOS=goos, GOARCH=goarch, CGO_ENABLED="0", GOWORK="off")
            notices = third_party_notices(source, env)
            subprocess.run(
                ["go", "build", "-trimpath", "-buildvcs=false", "-o", str(binary), "./cmd/braidplane"],
                cwd=source,
                env=env,
                check=True,
            )
            files = [
                (binary_name, binary.read_bytes(), 0o755),
                *shared,
                ("THIRD_PARTY_NOTICES.txt", notices, 0o644),
            ]
            base = f"braidplane_{version}_{goos}_{goarch}"
            archive = dist / (base + (".zip" if goos == "windows" else ".tar.gz"))
            (zip_archive if goos == "windows" else tar_archive)(archive, files)
            archives.append(archive)

    lines = []
    for archive in sorted(archives):
        checksum = hashlib.sha256(archive.read_bytes()).hexdigest()
        lines.append(f"{checksum}  {archive.name}\n")
    (dist / "checksums.txt").write_text("".join(lines), encoding="ascii")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--source", type=Path, default=Path.cwd())
    parser.add_argument("--dist", type=Path, default=Path("dist"))
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        if args.dist.exists():
            raise ReleaseError(f"distribution directory already exists: {args.dist}")
        build(args.source.resolve(), args.dist, args.tag)
        return 0
    except (ReleaseError, OSError, subprocess.CalledProcessError) as exc:
        print(f"release: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
