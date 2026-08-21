#!/usr/bin/env sh
#
# Run a command with the publishing credentials loaded from .env.
#
# npm does not read .env, and it reads a project .npmrc only from the directory
# it happens to be in -- which during a workspace publish is the package, not the
# workspace root. So this exports NPM_TOKEN for ${NPM_TOKEN} in the npmrc to
# expand against, and points npm at that npmrc explicitly, so it applies however
# deep the command ends up running.
#
# Pointing userconfig here also replaces ~/.npmrc for the duration, which is the
# safer default: this npmrc names registry.npmjs.org, so a publish cannot land in
# whatever private registry a developer is logged into.
set -e

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

if [ ! -f "$root/.env" ]; then
	echo "ts/.env is missing. It holds NPM_TOKEN; copy .env.example and fill it in." >&2
	exit 1
fi

set -a
. "$root/.env"
set +a

if [ -z "${NPM_TOKEN:-}" ]; then
	echo "NPM_TOKEN is empty in ts/.env." >&2
	exit 1
fi

# .npmrc is gitignored, so a fresh clone with a filled-in .env still has none --
# and npm treats a userconfig that does not exist as an empty one rather than as
# an error, which means the publish would fall through to whatever registry the
# developer is logged into. That is the failure this script exists to prevent,
# so it is checked rather than assumed.
if [ ! -f "$root/.npmrc" ]; then
	echo "ts/.npmrc is missing. It points npm at registry.npmjs.org; copy .npmrc.example." >&2
	exit 1
fi

export npm_config_userconfig="$root/.npmrc"
exec "$@"
