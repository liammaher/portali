#!/usr/bin/env python3
"""Tests of the setup skill's installer (install-pvault.sh) against a fake release: no network
beyond 127.0.0.1, no model.

  ~/.cache/pratica-portal/venv/bin/python -I plugin/test_install.py

Builds a real pvault for this machine (`go build`, version 9.9.9), packs it as goreleaser would
(pvault_9.9.9_<os>_<arch>.tar.gz), writes the checksums the way build.py pins them, serves the
release from a local server and runs the installer. Also: a download that doesn't match its
checksum, a checksum that is missing, a plugin built from source, and build.py's own checks of the
checksums it is given. install-pvault.ps1 can't run here: it is checked on Windows by hand.
"""

import hashlib
import io
import os
import platform
import shutil
import subprocess
import sys
import tarfile
import tempfile
import threading
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent
SKILL = HERE / "napoli" / "skills" / "napoli-setup"
sys.path.insert(0, str(HERE))
sys.dont_write_bytecode = True
import build  # noqa: E402

VERSION = "9.9.9"


def host():
    os_ = {"Darwin": "darwin", "Linux": "linux"}.get(platform.system())
    arch = {"arm64": "arm64", "aarch64": "arm64", "x86_64": "amd64", "AMD64": "amd64"}.get(platform.machine())
    if not os_ or not arch:
        sys.exit(f"the installer's shell script runs on macOS and Linux, not {platform.system()}/{platform.machine()}")
    return os_, arch


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def pack(binary, out, extra=b""):
    """A tar.gz as goreleaser makes it: pvault, LICENSE, README.md."""
    with tarfile.open(out, "w:gz") as t:
        t.add(binary, arcname="pvault")
        for name in ("LICENSE", "README.md"):
            data = (REPO / name).read_bytes() + extra
            info = tarfile.TarInfo(name)
            info.size = len(data)
            t.addfile(info, io.BytesIO(data))


class Quiet(SimpleHTTPRequestHandler):
    def log_message(self, *a):
        pass


def main():
    os_, arch = host()
    tmp = Path(tempfile.mkdtemp(prefix="install-test-"))
    failed, ran = [], []
    try:
        built = tmp / "pvault"
        subprocess.run(["go", "build", "-ldflags", f"-X main.version={VERSION}", "-o", str(built), "./cmd/pvault"],
                       cwd=REPO, check=True)
        asset = f"pvault_{VERSION}_{os_}_{arch}.tar.gz"
        release = tmp / "release" / f"v{VERSION}"
        release.mkdir(parents=True)
        pack(built, release / asset)

        # build.py wants all six archives in the checksums; the five that aren't this machine's are made up.
        sums = tmp / "checksums.txt"
        lines = []
        for o, a, ext in build.PVAULT_TARGETS:
            name = f"pvault_{VERSION}_{o}_{a}.{ext}"
            lines.append(f"{sha(release / asset) if name == asset else hashlib.sha256(name.encode()).hexdigest()}  {name}\n")
        lines.append(f"{hashlib.sha256(b'x').hexdigest()}  something_else.txt\n")
        sums.write_text("".join(lines))

        def skill_dir(name, with_release=True):
            d = tmp / name
            shutil.copytree(SKILL, d)
            if with_release:
                build.write_pvault_release(d, VERSION, sums)
            return d

        handler = lambda *a, **k: Quiet(*a, directory=str(tmp / "release"), **k)  # noqa: E731
        server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        base = f"http://127.0.0.1:{server.server_port}"

        def install(skill, dest, **env):
            e = {**os.environ, "PVAULT_RELEASE_BASE": base, "PVAULT_INSTALL_DIR": str(dest), **env}
            return subprocess.run(["sh", str(skill / "install-pvault.sh")], env=e, capture_output=True, text=True)

        def case(name, check):
            ran.append(name)
            try:
                check()
                print(f"ok    {name}")
            except AssertionError as e:
                failed.append(name)
                print(f"FAIL  {name}: {e}")

        def installs():
            dest = tmp / "bin1"
            r = install(skill_dir("s1"), dest)
            assert r.returncode == 0, r.stderr
            assert r.stdout.strip().splitlines()[-1] == f"PVAULT_PATH={dest / 'pvault'}", r.stdout
            out = subprocess.run([str(dest / "pvault"), "version"], capture_output=True, text=True).stdout.strip()
            assert out == f"pvault {VERSION}", out
            assert os.access(dest / "pvault", os.X_OK)
            again = install(skill_dir("s1b"), dest)
            assert again.returncode == 0 and "already installed" in again.stdout, again.stdout + again.stderr
        case("downloads the release, checks it, installs pvault and runs it", installs)

        def tampered():
            # The server now serves other bytes under the same name: the plugin's checksum refuses them.
            evil = tmp / "evil"
            evil.mkdir()
            pack(built, evil / asset, extra=b"\nnot the release\n")
            real = (release / asset).read_bytes()
            (release / asset).write_bytes((evil / asset).read_bytes())
            try:
                dest = tmp / "bin2"
                r = install(skill_dir("s2"), dest)
                assert r.returncode != 0 and "checksum mismatch" in r.stderr, (r.returncode, r.stderr)
                assert not (dest / "pvault").exists(), "a download that didn't match was installed"
            finally:
                (release / asset).write_bytes(real)
        case("a download that doesn't match the plugin's checksum installs nothing", tampered)

        def no_checksum():
            d = skill_dir("s3")
            (d / "pvault-checksums.txt").write_text("".join(l for l in (d / "pvault-checksums.txt").read_text().splitlines(True)
                                                            if asset not in l))
            dest = tmp / "bin3"
            r = install(d, dest)
            assert r.returncode != 0 and "no checksum for" in r.stderr and not (dest / "pvault").exists(), r.stderr
        case("an archive with no checksum in the plugin is refused", no_checksum)

        def from_source():
            dest = tmp / "bin4"
            r = install(skill_dir("s4", with_release=False), dest)
            assert r.returncode != 0 and "built from source" in r.stderr and not (dest / "pvault").exists(), r.stderr
        case("a plugin built from source says there is no release", from_source)

        def unreachable():
            dest = tmp / "bin5"
            r = install(skill_dir("s5"), dest, PVAULT_RELEASE_BASE="http://127.0.0.1:1")
            assert r.returncode != 0 and "download failed" in r.stderr and not (dest / "pvault").exists(), r.stderr
        case("a download that fails installs nothing", unreachable)

        def https_only():
            # Only a test sets PVAULT_RELEASE_BASE; otherwise the download is https, TLS 1.2 or later.
            text = (skill_dir("s6") / "install-pvault.sh").read_text()
            assert "proto='=https'" in text and "--tlsv1.2" in text
            assert text.count("proto='=https,http,file'") == 1 and "[ -z \"${PVAULT_RELEASE_BASE:-}\" ] ||" in text
        case("the installer asks for https unless a test overrides the base", https_only)

        def pins():
            d = tmp / "pin"
            d.mkdir()
            build.write_pvault_release(d, VERSION, sums)
            assert (d / "pvault.version").read_text() == VERSION + "\n"
            pinned = (d / "pvault-checksums.txt").read_text().splitlines()
            assert len(pinned) == 6 and all(l.split()[1].startswith("pvault_") for l in pinned), pinned
            for bad in ("0.1", "v1.0.0", "1.0.0; rm -rf"):
                try:
                    build.write_pvault_release(d, bad, sums)
                except SystemExit:
                    continue
                raise AssertionError(f"build.py took the version {bad!r}")
            short = tmp / "short.txt"
            short.write_text("".join(sums.read_text().splitlines(True)[1:]))
            try:
                build.write_pvault_release(d, VERSION, short)
            except SystemExit as e:
                assert "lacks" in str(e), e
            else:
                raise AssertionError("build.py took checksums missing an archive")
        case("build.py pins the six archives' checksums and refuses a short or odd set", pins)
    finally:
        shutil.rmtree(tmp, ignore_errors=True)
    print(f"{len(ran) - len(failed)}/{len(ran)} passed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
