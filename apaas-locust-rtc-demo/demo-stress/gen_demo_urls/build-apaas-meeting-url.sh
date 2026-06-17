#!/usr/bin/env bash
set -euo pipefail

generate_random_value() {
  local prefix="$1"
  local random_length="$2"

  RANDOM_PREFIX="${prefix}" RANDOM_LENGTH="${random_length}" python3 - <<'PY'
import os
import secrets
import string

prefix = os.environ["RANDOM_PREFIX"]

try:
    random_length = int(os.environ["RANDOM_LENGTH"])
except ValueError as exc:
    raise SystemExit(f"ERROR: random length must be an integer: {exc}")

if random_length < 1:
    raise SystemExit("ERROR: random length must be greater than 0")

alphabet = string.ascii_lowercase + string.digits
print(prefix + "".join(secrets.choice(alphabet) for _ in range(random_length)), end="")
PY
}

BASE_URL="${BASE_URL:-https://demo.edge.rtcdevelopers.com:8000}"
APP_PATH="${APP_PATH:-}"
VERSION="${VERSION:-}"
BUILD_ID="${BUILD_ID:-}"
PAGE="${PAGE:-recording}"

ENV_NAME="${ENV_NAME:-private}"
APP_ID="${APP_ID:-}"
ROOM_UUID="${ROOM_UUID:-}"
ROOM_NAME="${ROOM_NAME:-测试房间}"
ROOM_TYPE="${ROOM_TYPE:-11}"
USER_UUID_PREFIX="${USER_UUID_PREFIX:-apaas_}"
USER_NAME_PREFIX="${USER_NAME_PREFIX:-apaas_}"
RANDOM_LENGTH="${RANDOM_LENGTH:-10}"
USER_UUID="${USER_UUID:-$(generate_random_value "${USER_UUID_PREFIX}" "${RANDOM_LENGTH}")}"
USER_NAME="${USER_NAME:-$(generate_random_value "${USER_NAME_PREFIX}" "${RANDOM_LENGTH}")}"
RTM_TOKEN="${RTM_TOKEN:-}"
FETCH_RTM_TOKEN="${FETCH_RTM_TOKEN:-auto}"
TOKEN_API_HOST="${TOKEN_API_HOST:-}"
TOKEN_ROLE_ID="${TOKEN_ROLE_ID:-1}"
TOKEN_API_TIMEOUT="${TOKEN_API_TIMEOUT:-10}"
ROLE_TYPE="${ROLE_TYPE:-host}"
PRETEST="${PRETEST:-false}"
REGION="${REGION:-CN}"

read -r -d '' DEFAULT_SYSTEM_CONFIG <<'JSON' || true
{
    "rte": {
        "rteIpList": ["https://api-solutions.apaas1.ydjw.bj:18443"],
        "rtmIpList": ["20-1-126-40.edge.rtm.ydjw.bj:8443"],
        "rtmOriginDomains": ["rtm.ydjw.bj"],
        "rtcInternalApConfig": {
            "serverList": ["20.1.125.168"],
            "verifyDomainName": "ap.1452738.agora.local"
        },
        "rtcWebInternalApConfig": {
            "serverDomain": "edge.rtc.ydjw.bj",
            "serverList": ["20-1-125-168.edge.rtc.ydjw.bj"],
            "serverPort": 8443
        }
    },
    "core": {
        "coreIpList": [
            "https://api-solutions.apaas1.ydjw.bj:18443"],
        "easemobChatIpList": [
            "ws://20.1.125.245:12003/websocket"
        ],
        "easemobRestIpList": [
            "http://20.1.125.245:80"
        ]
    }
}

JSON

SYSTEM_CONFIG="${SYSTEM_CONFIG:-$DEFAULT_SYSTEM_CONFIG}"
SYSTEM_CONFIG_FILE="${SYSTEM_CONFIG_FILE:-}"

export BASE_URL APP_PATH VERSION BUILD_ID PAGE
export ENV_NAME APP_ID ROOM_UUID ROOM_NAME ROOM_TYPE USER_UUID USER_NAME
export USER_UUID_PREFIX USER_NAME_PREFIX RANDOM_LENGTH
export RTM_TOKEN FETCH_RTM_TOKEN TOKEN_API_HOST TOKEN_ROLE_ID TOKEN_API_TIMEOUT
export ROLE_TYPE PRETEST REGION SYSTEM_CONFIG SYSTEM_CONFIG_FILE

usage() {
  cat <<EOF
Usage:
  build-apaas-meeting-url.sh
    --app-id '<appId>'
    --host '<token-api-host>'
    --room-uuid '<roomUuid>'
    [--room-name '<roomName>']
    [--user-uuid '<userUuid>']
    [--user-name '<userName>']
    [--rtm-token '<rtmToken>']
  build-apaas-meeting-url.sh --parse '<url>'

Build mode reads values from environment variables:
  BASE_URL       default: ${BASE_URL}
  APP_PATH       default: ${APP_PATH}
  VERSION        default: ${VERSION}
  BUILD_ID       default: ${BUILD_ID}
  PAGE           default: ${PAGE}
  ENV_NAME       default: ${ENV_NAME}
  APP_ID         required, or pass --app-id
  ROOM_UUID      required, or pass --room-uuid
  ROOM_NAME
  ROOM_TYPE
  USER_UUID      default: ${USER_UUID_PREFIX}<10 random lowercase letters or digits>
  USER_NAME      default: ${USER_NAME_PREFIX}<10 random lowercase letters or digits>
  USER_UUID_PREFIX
                 default: ${USER_UUID_PREFIX}
  USER_NAME_PREFIX
                 default: ${USER_NAME_PREFIX}
  RANDOM_LENGTH  default: ${RANDOM_LENGTH}
  RTM_TOKEN      optional; if empty, the script fetches it from token API by default
  FETCH_RTM_TOKEN
                 auto | true | false, default: auto
                 auto means fetch only when RTM_TOKEN is empty
  TOKEN_API_HOST required, or pass --host
  TOKEN_ROLE_ID  default: ${TOKEN_ROLE_ID}
  TOKEN_API_TIMEOUT
                 default: 10 seconds
  ROLE_TYPE      default: ${ROLE_TYPE}
  PRETEST        default: false
  REGION         default: CN
  SYSTEM_CONFIG  normal JSON string, not URL-encoded
  SYSTEM_CONFIG_FILE
                 optional JSON file path; overrides SYSTEM_CONFIG

Example:
  ./scripts/build-apaas-meeting-url.sh \\
    --app-id '50b7cb2ccd4f46f7936d0e2a52e56d1d' \\
    --host 'https://api-solutions-private.agoralab.co' \\
    --room-uuid '993201211'

Fetch token example:
  APP_ID='50b7cb2ccd4f46f7936d0e2a52e56d1d' \\
    TOKEN_API_HOST='https://api-solutions-private.agoralab.co' \\
    ROOM_UUID='993201211' \\
    ./scripts/build-apaas-meeting-url.sh

System config examples:
  SYSTEM_CONFIG='{"rte":{"rtmIpList":["apaas-private-ap.edge.agora.io"],"rtmOriginDomains":["agora.io"]}}' \\
    APP_ID='50b7cb2ccd4f46f7936d0e2a52e56d1d' \\
    TOKEN_API_HOST='https://api-solutions-private.agoralab.co' \\
    ROOM_UUID='993201211' \\
    RTM_TOKEN='xxx' ./scripts/build-apaas-meeting-url.sh

  SYSTEM_CONFIG_FILE='./system-config.json' RTM_TOKEN='xxx' ./scripts/build-apaas-meeting-url.sh

Parse example:
  ./scripts/build-apaas-meeting-url.sh --parse 'https://.../index.html#/recording?...'
EOF
}

require_non_empty() {
  local name="$1"
  local value="$2"

  if [[ -z "${value}" ]]; then
    echo "ERROR: ${name} is required." >&2
    exit 1
  fi
}

fetch_rtm_token() {
  python3 - <<'PY'
import json
import os
import sys
from urllib.error import HTTPError, URLError
from urllib.parse import quote
from urllib.request import Request, urlopen


def find_token(value):
    if isinstance(value, dict):
        for key in ("rtmToken", "rtm_token", "token", "accessToken"):
            token = value.get(key)
            if isinstance(token, str) and token:
                return token
        for nested in value.values():
            token = find_token(nested)
            if token:
                return token
    if isinstance(value, list):
        for item in value:
            token = find_token(item)
            if token:
                return token
    return None


host = os.environ["TOKEN_API_HOST"].rstrip("/")
room_uuid = quote(os.environ["ROOM_UUID"], safe="")
role_id = quote(os.environ["TOKEN_ROLE_ID"], safe="")
user_uuid = quote(os.environ["USER_UUID"], safe="")
timeout = float(os.environ["TOKEN_API_TIMEOUT"])
url = f"{host}/conference/v3/rooms/{room_uuid}/roles/{role_id}/users/{user_uuid}/token"

request = Request(url, headers={"Accept": "application/json"})

try:
    with urlopen(request, timeout=timeout) as response:
        body = response.read().decode("utf-8")
except HTTPError as exc:
    detail = exc.read().decode("utf-8", errors="replace")
    raise SystemExit(f"ERROR: token API returned HTTP {exc.code}: {detail[:500]}")
except URLError as exc:
    raise SystemExit(f"ERROR: failed to request token API {url}: {exc}")

body = body.strip()
if not body:
    raise SystemExit("ERROR: token API returned empty response")

try:
    payload = json.loads(body)
except json.JSONDecodeError:
    print(body, end="")
    sys.exit(0)

token = payload if isinstance(payload, str) else find_token(payload)
if not isinstance(token, str) or not token:
    raise SystemExit(f"ERROR: token API response does not contain a token field: {body[:500]}")

print(token, end="")
PY
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ "${1:-}" == "--parse" ]]; then
  if [[ -z "${2:-}" ]]; then
    echo "ERROR: --parse requires a URL." >&2
    exit 1
  fi
  URL_TO_PARSE="${2}" python3 - <<'PY'
import json
import os
from urllib.parse import parse_qsl, unquote, urlparse

url = os.environ["URL_TO_PARSE"]
parsed = urlparse(url)
hash_value = parsed.fragment
route, _, query = hash_value.partition("?")
params = dict(parse_qsl(query, keep_blank_values=True))

result = {
    "protocol": parsed.scheme,
    "host": parsed.netloc,
    "path": parsed.path,
    "hashRoute": route,
    "params": params,
}

system_config = params.get("systemConfig")
if system_config:
    try:
        result["systemConfigParsed"] = json.loads(system_config)
    except json.JSONDecodeError:
        result["systemConfigParsed"] = "invalid JSON"

print(json.dumps(result, ensure_ascii=False, indent=2))
PY
  exit 0
fi

while [[ $# -gt 0 ]]; do
  case "$1" in
    --app-id)
      APP_ID="${2:-}"
      shift 2
      ;;
    --host|--token-api-host)
      TOKEN_API_HOST="${2:-}"
      shift 2
      ;;
    --room-uuid)
      ROOM_UUID="${2:-}"
      shift 2
      ;;
    --room-name)
      ROOM_NAME="${2:-}"
      shift 2
      ;;
    --room-type)
      ROOM_TYPE="${2:-}"
      shift 2
      ;;
    --user-uuid)
      USER_UUID="${2:-}"
      shift 2
      ;;
    --user-name)
      USER_NAME="${2:-}"
      shift 2
      ;;
    --rtm-token)
      RTM_TOKEN="${2:-}"
      shift 2
      ;;
    --system-config)
      SYSTEM_CONFIG="${2:-}"
      shift 2
      ;;
    --system-config-file)
      SYSTEM_CONFIG_FILE="${2:-}"
      shift 2
      ;;
    --base-url)
      BASE_URL="${2:-}"
      shift 2
      ;;
    --version)
      VERSION="${2:-}"
      shift 2
      ;;
    --build-id)
      BUILD_ID="${2:-}"
      shift 2
      ;;
    --)
      shift
      break
      ;;
    *)
      echo "ERROR: unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

export BASE_URL APP_PATH VERSION BUILD_ID PAGE
export ENV_NAME APP_ID ROOM_UUID ROOM_NAME ROOM_TYPE USER_UUID USER_NAME
export USER_UUID_PREFIX USER_NAME_PREFIX RANDOM_LENGTH
export RTM_TOKEN FETCH_RTM_TOKEN TOKEN_API_HOST TOKEN_ROLE_ID TOKEN_API_TIMEOUT
export ROLE_TYPE PRETEST REGION SYSTEM_CONFIG SYSTEM_CONFIG_FILE

require_non_empty "APP_ID or --app-id" "${APP_ID}"
require_non_empty "TOKEN_API_HOST or --host" "${TOKEN_API_HOST}"
require_non_empty "ROOM_UUID or --room-uuid" "${ROOM_UUID}"

case "${FETCH_RTM_TOKEN}" in
  auto)
    if [[ -z "${RTM_TOKEN}" ]]; then
      RTM_TOKEN="$(fetch_rtm_token)"
      export RTM_TOKEN
    fi
    ;;
  true)
    RTM_TOKEN="$(fetch_rtm_token)"
    export RTM_TOKEN
    ;;
  false)
    ;;
  *)
    echo "ERROR: FETCH_RTM_TOKEN must be one of: auto, true, false." >&2
    exit 1
    ;;
esac

python3 - <<'PY'
import json
import os
from urllib.parse import urlencode

base_url = os.environ["BASE_URL"].rstrip("/")
app_path = "/" + os.environ["APP_PATH"].strip("/")
version = os.environ["VERSION"].strip("/")
build_id = os.environ["BUILD_ID"].strip("/")
page = os.environ["PAGE"].strip("/")

system_config_file = os.environ.get("SYSTEM_CONFIG_FILE", "")
if system_config_file:
    try:
        with open(system_config_file, "r", encoding="utf-8") as file:
            system_config_raw = file.read()
    except OSError as exc:
        raise SystemExit(f"ERROR: failed to read SYSTEM_CONFIG_FILE: {exc}")
else:
    system_config_raw = os.environ["SYSTEM_CONFIG"]

try:
    system_config = json.dumps(json.loads(system_config_raw), ensure_ascii=False, separators=(",", ":"))
except json.JSONDecodeError as exc:
    source = f"SYSTEM_CONFIG_FILE={system_config_file}" if system_config_file else "SYSTEM_CONFIG"
    raise SystemExit(f"ERROR: {source} is not valid JSON: {exc}")

params = {
    "env": os.environ["ENV_NAME"],
    "appId": os.environ["APP_ID"],
    "roomUuid": os.environ["ROOM_UUID"],
    "roomName": os.environ["ROOM_NAME"],
    "roomType": os.environ["ROOM_TYPE"],
    "userUuid": os.environ["USER_UUID"],
    "userName": os.environ["USER_NAME"],
    "rtmToken": os.environ["RTM_TOKEN"],
    "roleType": os.environ["ROLE_TYPE"],
    "pretest": os.environ["PRETEST"],
    "region": os.environ["REGION"],
    "systemConfig": system_config,
}

query = urlencode(params, safe="")
print(f"{base_url}/index.html#/{page}?{query}")
PY
