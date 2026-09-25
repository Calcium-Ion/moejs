#!/usr/bin/env bash
# Downloads the new-api task plugins at the commit pinned below into
# bench/testdata/plugins/<key>/plugin.js and checks each file against its git
# blob hash. The fixtures in bench/testdata/fixtures were recorded against
# exactly these files, so change REV and the hashes only together with a new
# recording (go run ./cmd/recordfixtures in bench/).
#
# Usage: bench/testdata/plugins/fetch.sh
set -euo pipefail

REV=474ed66fb14b0de30f82a1729b4f7cf3dab19d63
BASE="https://raw.githubusercontent.com/QuantumNous/new-api/$REV/plugins/tasks"
HERE="$(cd "$(dirname "$0")" && pwd)"

while read -r key blob; do
	dest="$HERE/$key/plugin.js"
	if [ -f "$dest" ] && [ "$(git hash-object "$dest")" = "$blob" ]; then
		continue
	fi
	mkdir -p "$HERE/$key"
	curl -fsSL "$BASE/$key/plugin.js" -o "$dest.tmp"
	if [ "$(git hash-object "$dest.tmp")" != "$blob" ]; then
		rm -f "$dest.tmp"
		echo "$key: downloaded plugin.js does not match $blob" >&2
		exit 1
	fi
	mv "$dest.tmp" "$dest"
	echo "fetched $key"
done <<'EOF'
alibaba 7af288807ce55cfbbc2ae0ed1b836be96553ae8e
doubao d50c2c86c0f7c47eb5171f905ec1e78267964b4e
google 64affc9a53070f84131b5c6a453ef8d654bb9cc2
hailuo 123a778819ed8fe396efc7b3ca4491cc641e93ba
jimeng 63565827dc08d74d8deaff9f0915531a1b91c4ca
kling 6953c92129dfc2738f759c8eb896f8346febd9df
sora ed589ca2f8aeb448b49b0fe33707346bd5781434
sunoapi db25b19ae28efae727d3f3dbc423f40ee0199e3e
vertex-ai 1d432d56a75c2888d52fd652ee11338d12c7c52f
vidu 80aa5fdb24c99ea3bc3e134d9aeb2ca1d2726415
EOF
echo "new-api plugins at ${REV:0:9} are in $HERE"
