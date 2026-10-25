#!/usr/bin/env bash
# C7b end-to-end: a real Envoy in a container, the dqextprocd service in another container on the
# same network, and a route that answers 201. A 201 means the filter forwarded; a 422 or 415 means
# the filter answered inside the proxy and the route never ran. That difference is the
# interception claim.
#
# This script exists because a unit test with a fake stream proves the server answers correctly to
# messages I wrote. It cannot prove that Envoy sends those messages, at that phase, with the body
# buffered — which is the part most likely to be wrong. The first run of this harness failed on
# exactly that class of problem: Envoy resolved the host gateway to IPv6 while the daemon listened
# on IPv4, and no unit test would have found it.
set -euo pipefail

ROOT=${DQ_ROOT:-examples/party-model}
PORT=18000
HITS_PORT=18001
IMG=${ENVOY_IMAGE:-envoyproxy/envoy:v1.31-latest}
NET="dq-e2e-$$"
EXTPROC="dq-extproc-$$"
ENVOY="dq-envoy-$$"
UPSTREAM="dq-upstream-$$"
WORK=$(mktemp -d)

cleanup() {
  docker rm -f "$ENVOY" "$EXTPROC" "$UPSTREAM" >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT

pass() { printf '  ok   %s\n' "$1"; }
fail() { printf '  FAIL %s\n' "$1"; exit 1; }

echo "building the ext_proc daemon and the upstream stub for linux/$(go env GOARCH)"
GOOS=linux GOARCH="$(go env GOARCH)" go build -o "$WORK/dqextprocd" ./tools/dqextprocd
GOOS=linux GOARCH="$(go env GOARCH)" go build -o "$WORK/e2estub" ./ci/e2e/stub
chmod +x "$WORK/dqextprocd" "$WORK/e2estub"

docker network create "$NET" >/dev/null

echo "starting the upstream stub (it counts what reaches it)"
docker run -d --rm --name "$UPSTREAM" --network "$NET" --network-alias upstream \
  -p 18001:8080 \
  -v "$WORK/e2estub:/usr/local/bin/e2estub:ro" \
  alpine:latest /usr/local/bin/e2estub >/dev/null

echo "starting dqextprocd in a container"
docker run -d --rm --name "$EXTPROC" --network "$NET" --network-alias extproc \
  -v "$WORK/dqextprocd:/usr/local/bin/dqextprocd:ro" \
  -v "$PWD/$ROOT:/tree:ro" \
  alpine:latest /usr/local/bin/dqextprocd --root /tree --addr :8081 --mode ENFORCE >/dev/null

for _ in $(seq 1 50); do
  docker logs "$EXTPROC" 2>&1 | grep -q 'deciding /' && break
  sleep 0.2
done
docker logs "$EXTPROC" 2>&1 | grep -q 'deciding /' || { docker logs "$EXTPROC" 2>&1 | tail -20; fail "the daemon did not start"; }
docker logs "$EXTPROC" 2>&1 | sed 's/^/       /'

echo "starting envoy ($IMG)"
docker run -d --rm --name "$ENVOY" --network "$NET" --network-alias envoy \
  -p "$PORT:8000" \
  -v "$PWD/ci/e2e/envoy.yaml:/etc/envoy/envoy.yaml:ro" \
  "$IMG" -c /etc/envoy/envoy.yaml --log-level warn >/dev/null

echo "waiting for :$PORT"
for _ in $(seq 1 100); do
  if curl -s -o /dev/null "http://127.0.0.1:$PORT/" 2>/dev/null; then break; fi
  sleep 0.2
done
curl -s -o /dev/null "http://127.0.0.1:$PORT/" || { docker logs "$ENVOY" 2>&1 | tail -20; fail "envoy did not come up"; }

valid='{"party":{"partyId":"PTY-00000001","individual":{"names":[{"type":"FIRST","value":"Amara"},{"type":"LAST","value":"Okafor"}]}},"relationship":{"type":"CUSTOMER","lineOfBusiness":"RETAIL"},"contactPoints":[{"partyId":"PTY-00000001","contacts":[{"type":"CORRESPONDENCE","address":{"street1":"14 Marina Road","city":"Lagos","postalCode":"101241","country":"NG"}}]}]}'
invalid='{"party":{"partyId":"PTY-00000004","individual":{"names":[{"type":"LAST","value":"Okafor"}]}},"relationship":{"partyId":"PTY-00000004","type":"CUSTOMER","lineOfBusiness":"RETAIL"},"contactPoints":[{"partyId":"PTY-00000004","contacts":[{"type":"MOBILE","address":{"street1":"1 X","city":"Lagos","postalCode":"101241","country":"NG"}}]}]}'

echo "assertions"
# The upstream is queried directly, not through Envoy: measuring it through the system under
# test would measure the system under test — and it fails closed, so the probe would fail too.
hits() { curl -s "http://127.0.0.1:$HITS_PORT/__hits"; }
before=$(hits)

# 1. a clean record reaches the upstream
code=$(curl -s -o "$WORK/b1" -w '%{http_code}' -D "$WORK/h1" -X POST \
  -H 'content-type: application/json' --data "$valid" "http://127.0.0.1:$PORT/onboard-party")
[[ "$code" == "201" ]] || { cat "$WORK/b1"; fail "clean record: want 201 from the upstream, got $code"; }
grep -qi '^DQ-Pack-Digest: sha256:' "$WORK/h1" || fail "clean record: no DQ-Pack-Digest on the response"
after=$(hits)
[[ "$after" == "$((before + 1))" ]] || fail "clean record: upstream went $before -> $after, expected +1"
pass "a clean record reaches the upstream, which records it, and the caller gets the digest"

# 2. an invalid record is refused inside the proxy — the upstream must not see it
before=$(hits)
code=$(curl -s -o "$WORK/b2" -w '%{http_code}' -X POST \
  -H 'content-type: application/json' --data "$invalid" "http://127.0.0.1:$PORT/onboard-party")
[[ "$code" == "422" ]] || { cat "$WORK/b2"; fail "invalid record: want 422 from the filter, got $code"; }
grep -q '"violations"' "$WORK/b2" || fail "invalid record: the body is not a problem document"
[[ "$(hits)" == "$before" ]] || fail "invalid record: the upstream was called — the filter did not intercept"
pass "an invalid record is refused before the upstream, which never sees it"

# 3. a body that is not JSON is a platform failure, not a violation
before=$(hits)
code=$(curl -s -o "$WORK/b3" -w '%{http_code}' -X POST \
  -H 'content-type: application/json' --data 'not json' "http://127.0.0.1:$PORT/onboard-party")
[[ "$code" == "415" ]] || { cat "$WORK/b3"; fail "non-JSON: want 415, got $code"; }
grep -q 'DQ_UNSUPPORTED_MEDIA_TYPE' "$WORK/b3" || fail "non-JSON: wrong failure code"
[[ "$(hits)" == "$before" ]] || fail "non-JSON: the upstream was called"
pass "a non-JSON body is 415, and says it could not tell rather than blaming the data"

# 4. an unrouted path is not decided
before=$(hits)
code=$(curl -s -o "$WORK/b4" -w '%{http_code}' -X POST \
  -H 'content-type: application/json' --data 'not json' "http://127.0.0.1:$PORT/anything-else")
[[ "$code" == "201" ]] || { cat "$WORK/b4"; fail "unrouted: want the upstream's 201, got $code"; }
[[ "$(hits)" == "$((before + 1))" ]] || fail "unrouted: the upstream was not reached"
pass "an unrouted path passes through untouched"

# 5. concurrency smoke: 300 requests through the proxy at 20 in flight, and every one must be
# answered. This asserts the only thing this environment can assert. It does *not* measure the
# latency budget: run-to-run variance here is larger than the 1 ms the design budgets (two runs
# against the same endpoint differed 10x at p99), so a threshold would be asserting noise.
echo "concurrency smoke (T5 latency is not measurable here — see the note it prints)"
GOOS=linux GOARCH="$(go env GOARCH)" go build -o "$WORK/e2eload" ./ci/e2e/load
chmod +x "$WORK/e2eload"
printf '%s' "$valid" > "$WORK/payload.json"
out=$(docker run --rm --network "$NET" \
  -v "$WORK/e2eload:/bin/e2eload:ro" -v "$WORK/payload.json:/payload.json:ro" \
  alpine:latest /bin/e2eload \
  --direct http://upstream:8080/onboard-party \
  --proxied http://envoy:8000/onboard-party \
  --payload /payload.json --n "${LOAD_N:-300}" --c "${LOAD_C:-20}" 2>&1) || { echo "$out" | sed 's/^/       /'; fail "requests failed under concurrency"; }
echo "$out" | sed 's/^/       /'
pass "every request was answered under concurrency"

# 6. the filter's absence rejects traffic, because §2.7 sets failure_mode_allow: false.
# Envoy's own status varies with how the call fails — 500 when it cannot connect, 504 when the
# message timeout expires — so the assertion is that it fails closed at all, not which 5xx.
before=$(hits)
docker rm -f "$EXTPROC" >/dev/null
sleep 1
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST \
  -H 'content-type: application/json' --data "$valid" "http://127.0.0.1:$PORT/onboard-party")
[[ "$code" == 5?? ]] || fail "filter down: want a 5xx from Envoy, got $code"
[[ "$(hits)" == "$before" ]] || fail "filter down: the upstream was called anyway"
pass "with the filter gone, ENFORCE fails closed (${code}) rather than passing the record"

echo "all six assertions held: envoy intercepts, validates, answers under concurrency, and fails closed"
