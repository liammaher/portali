#!/usr/bin/env bash
# The Mod. 1 pilot: fill the TARI Mod. 1 from a made-up vault, end to end.
#
#   PRATICHE_NAPOLI_DIR=<pratiche-napoli checkout> run.sh [output dir]
#
# Reads the library entry, downloads the form from its official URL (fill.py
# refuses it unless its sha256 is the entry's), starts a synthetic vault
# (internal/api TestPraticaPilotVault) with the tokens fill.py needs, and runs
# fill.py against it. Nothing leaves this machine except the form download.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../../../../.." && pwd)
library=${PRATICHE_NAPOLI_DIR:?set PRATICHE_NAPOLI_DIR to a pratiche-napoli checkout}
id=napoli/comune/tari-iscrizione-uso-abitativo
form=mod-1
layout="$library/layouts/$id/$form.json"
out=${1:-$(mktemp -d)}
mkdir -p "$out"
# The harness's files, its tokens among them, go when the run does.
work=$(mktemp -d)
harness=
trap 'touch "$work/done"; if [ -n "$harness" ]; then wait "$harness" || true; fi; rm -rf "$work"' EXIT

(cd "$library" && node --input-type=module -e '
  import { parse } from "yaml";
  import { readFileSync } from "node:fs";
  process.stdout.write(JSON.stringify(parse(readFileSync(process.argv[1], "utf8"), { version: "1.2" })));
' "procedures/$id.yaml") > "$work/entry.json"

python3 -I "$here/../fill.py" needs --entry "$work/entry.json" --form "$form" > "$work/needs.json"
cp "$here/fixture.json" "$work/"

url=$(python3 -I -c 'import json,sys; print(next(f["url"] for f in json.load(open(sys.argv[1]))["forms"] if f["id"] == sys.argv[2]))' "$work/entry.json" "$form")
curl -fsSL -o "$work/form.pdf" "$url"

(cd "$repo" && PVAULT_PILOT_DIR="$work" go test ./internal/api -run '^TestPraticaPilotVault$' -count=1 -timeout 15m) > "$work/harness.log" 2>&1 &
harness=$!
until [ -s "$work/server.json" ]; do
  if ! kill -0 "$harness" 2>/dev/null; then cat "$work/harness.log"; exit 1; fi
  sleep 0.2
done

echo "scopes: $(python3 -I -c 'import json,sys; t=json.load(open(sys.argv[1]))["tokens"]; print("; ".join(f"{k} {v["access"]} {v["scope"]}" for k,v in t.items()))' "$work/needs.json")"
python3 -I "$here/../fill.py" run --entry "$work/entry.json" --form "$form" --layout "$layout" \
  --server "$work/server.json" --pdf "$work/form.pdf" --out "$out" \
  --answers "$here/answers.json" --reply "$here/reply.json" --playbook napoli-tari-iscrizione
