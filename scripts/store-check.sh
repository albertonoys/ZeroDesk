#!/usr/bin/env bash
# Run the CardputerZero Store's checks on the Store edition .deb, locally.
# Nothing is uploaded and no pull request is opened.
# Usage: scripts/store-check.sh dist/zerodesk-cp0_<version>-<rev>_arm64.deb [github-login]
# Needs Docker. Clones CardputerZero/AppBuilder and CardputerZero/packages into .cache/.
set -euo pipefail
cd "$(dirname "$0")/.."
DEB="${1:?usage: scripts/store-check.sh <deb> [github-login]}"
LOGIN="${2:-richardhxwang}"
mkdir -p .cache
for repo in AppBuilder packages; do
	if [ -d ".cache/$repo/.git" ]; then
		git -C ".cache/$repo" fetch -q --depth 1 origin && git -C ".cache/$repo" reset -q --hard origin/HEAD
	else
		git clone -q --depth 1 "https://github.com/CardputerZero/$repo.git" ".cache/$repo"
	fi
done
docker run --rm --platform linux/arm64 -v "$PWD:/app:ro" debian:trixie bash -c "
	apt-get update -qq && apt-get install -y -qq python3 dpkg-dev >/dev/null &&
	python3 /app/scripts/store_check.py '/app/$DEB' /app /app/.cache/AppBuilder /app/.cache/packages '$LOGIN'"
