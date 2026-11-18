#!/usr/bin/env python3
"""Run the CardputerZero Store's checks on a .deb locally, without publishing.

The same checks as `czdev publish` (before it uploads anything or opens a PR)
and the packages repository's validate-pr workflow, with the policy scripts
taken from local clones of CardputerZero/AppBuilder and CardputerZero/packages.
Needs dpkg-deb: run it in Debian (scripts/store-check.sh does that in Docker).

Usage: store_check.py <deb> <app-dir> <AppBuilder-clone> <packages-clone> <github-login>
"""
import json
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from types import SimpleNamespace

deb, app_dir, appbuilder, packages, login = sys.argv[1:6]
sys.path.insert(0, str(Path(appbuilder) / "scripts"))
from czdev import publish  # noqa: E402

# czdev would fetch these from GitHub; use the local clone instead
publish.fetch_url = lambda url, timeout=15: None

failures = []


def ok(msg):
    print(f"  ✓ {msg}")


def fail(msg):
    print(f"  ✗ {msg}")
    failures.append(msg)


print(f"Package: {deb}\n\nczdev preflight:")
if publish.check_desktop(deb):
    ok(".desktop file found")
else:
    fail("no usr/share/APPLaunch/applications/*.desktop")
roots = publish.find_root_services(deb)
if roots:
    fail(f"systemd services run as root: {roots}")
else:
    ok("no root systemd services")
meta = publish.extract_metadata(deb)
ok(f"Maintainer: {meta['maintainer']}")
if publish.is_valid_package_name(meta["package"]):
    ok(f"package name {meta['package']!r} is valid")
else:
    fail(f"invalid package name {meta['package']!r}")
size = Path(deb).stat().st_size
ok(f"version {meta['version']}, arch {meta['architecture']}, {size / 1048576:.1f} MB")

raw, found_dir = publish.load_app_builder(Path(app_dir))
store_meta = publish.store_meta_from(raw)
meta_out = publish.build_merged_meta(store_meta, meta, SimpleNamespace(login=login, email=None))

print("\nstore metadata policy (packages/.github/scripts/store_meta_policy.py, with store-wide uniqueness):")
stage = Path(tempfile.mkdtemp(prefix="store-check-"))
try:
    pkg_dir = stage / meta["package"]
    (pkg_dir / "screenshots").mkdir(parents=True)
    (pkg_dir / "meta.json").write_text(json.dumps(meta_out, ensure_ascii=False))
    for shot in store_meta.get("screenshots") or []:
        shutil.copy2(Path(found_dir) / shot, pkg_dir / "screenshots" / Path(shot).name)
    if store_meta.get("icon"):
        shutil.copy2(Path(found_dir) / store_meta["icon"], pkg_dir / Path(store_meta["icon"]).name)
    errs, warns = stage / "errors.txt", stage / "warnings.txt"
    env = {"DEB_MAINTAINER": meta["maintainer"], "PATH": "/usr/bin:/bin"}
    subprocess.run([sys.executable, f"{packages}/.github/scripts/store_meta_policy.py", meta["package"], str(pkg_dir),
                    f"{packages}/pool/main", str(errs), str(warns)], check=True, env=env)
    for line in (warns.read_text().splitlines() if warns.exists() else []):
        print(f"  ⚠ {line}")
    if errs.exists() and errs.read_text().strip():
        for line in errs.read_text().splitlines():
            fail(line)
    else:
        ok("store metadata passes")
finally:
    shutil.rmtree(stage, ignore_errors=True)

print("\npackages validate-pr checks:")
fname = Path(deb).name
arch = subprocess.run(["dpkg-deb", "-f", deb, "Architecture"], capture_output=True, text=True).stdout.strip()
ok(f"architecture {arch}") if arch in ("arm64", "all") else fail(f"architecture {arch}")
contents = subprocess.run(["dpkg-deb", "-c", deb], capture_output=True, text=True).stdout
perms = [l.split()[0] for l in contents.splitlines() if l.strip()]
if any(re.match(r"^[-l].{2}[sS]|^[-l].{5}[sS]", p) for p in perms):
    fail("setuid/setgid files")
else:
    ok("no setuid/setgid files")
if any(p[0] in "bc" for p in perms):
    fail("device files")
else:
    ok("no device files")
bad, helpmd = stage.with_suffix(".bad"), stage.with_suffix(".help")
subprocess.run([sys.executable, f"{packages}/.github/scripts/install_path_policy.py", meta["package"], deb, str(bad), str(helpmd)], check=True)
if bad.exists() and bad.read_text().strip():
    for line in bad.read_text().splitlines():
        fail("install path: " + line)
else:
    ok("install paths allowed")
ctrl = Path(tempfile.mkdtemp(prefix="store-ctrl-"))
subprocess.run(["dpkg-deb", "-e", deb, str(ctrl)], check=True)
danger = re.compile(r"rm[ \t]+(-[a-zA-Z]+[ \t]+)*(/([ \t]|$)|/(bin|etc|usr|var|home|boot|lib))|(curl|wget)[^|]*\|[ \t]*(ba)?sh|mkfs|/etc/(passwd|shadow|sudoers)|dd[ \t][^|]*of=/dev/", re.M)
for scr in ("preinst", "postinst", "prerm", "postrm"):
    f = ctrl / scr
    if f.exists():
        if danger.search(f.read_text()):
            fail(f"dangerous pattern in {scr}")
        else:
            ok(f"{scr} has no dangerous patterns")
if re.match(r"^[a-z0-9][a-z0-9.+\-]+$", meta["package"]):
    ok("package name matches the store's pattern")
else:
    fail("package name pattern")
expected = f"{meta['package']}_{meta['version']}_{arch}.deb"
if fname in (expected, re.sub(r"[~:]", ".", expected)):
    ok(f"file name {fname}")
else:
    fail(f"file name {fname!r} should be {expected!r}")

print()
if failures:
    print(f"FAILED: {len(failures)} problem(s)")
    sys.exit(1)
print("All Store checks passed.")
