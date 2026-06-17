#!/usr/bin/env python3
import json
import os
import time
import uuid
import urllib.error
import urllib.request
import traceback

from src.apaas_token_builder import ApaasTokenBuilder


def request_json(method, host, path, user_uuid, token=None, payload=None, timeout=20):
    url = host.rstrip("/") + path
    data = None
    headers = {}
    if token:
        headers["Authorization"] = f'agora token="{token}"'
        headers["X-Agora_token"] = token
        headers["X-Agora-Uid"] = user_uuid
    if payload is not None:
        data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"

    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    start = time.time()
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            body = resp.read().decode("utf-8", "ignore")
            cost_ms = (time.time() - start) * 1000
            print(f"{method} {path} -> HTTP {resp.status} cost={cost_ms:.1f}ms body={body[:800]}", flush=True)
            try:
                return resp.status, json.loads(body) if body else {}
            except Exception:
                return resp.status, {"raw": body}
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", "ignore")
        cost_ms = (time.time() - start) * 1000
        print(f"{method} {path} -> HTTPError {exc.code} cost={cost_ms:.1f}ms body={body[:1200]}", flush=True)
        try:
            return exc.code, json.loads(body) if body else {}
        except Exception:
            return exc.code, {"raw": body}


def main():
    host = os.environ.get("APAAS_HOST", "http://172.31.11.139")
    app_id = os.environ["AGORA_APP_ID"]
    app_certificate = os.environ["AGORA_APP_CERTIFICATE"]
    room_uuid = "smoke_" + uuid.uuid4().hex
    user_uuid = "user_" + uuid.uuid4().hex

    print(f"host={host}", flush=True)
    print(f"app_id={app_id}", flush=True)
    print(f"room_uuid={room_uuid}", flush=True)
    print(f"user_uuid={user_uuid}", flush=True)

    server_token = ApaasTokenBuilder.build_app_token(app_id, app_certificate, 6000)
    print(f"server_token_ok={bool(server_token)}", flush=True)

    _, result = request_json(
        "POST",
        host,
        f"/cn/conference/apps/{app_id}/v1/rooms/{room_uuid}",
        user_uuid,
        server_token,
        {
            "roomName": f"房间{room_uuid}",
            "roomProperties": {},
            "roomTemplate": "conf_finity_v1",
        },
    )
    print(f"create_result_code={result.get('code')} message={result.get('message')}", flush=True)

    _, result = request_json(
        "GET",
        host,
        f"/conference/v3/rooms/{room_uuid}/roles/3/users/{user_uuid}/token",
        user_uuid,
    )
    user_token = result.get("data", {}).get("token")
    print(f"room_user_token_ok={bool(user_token)} code={result.get('code')} message={result.get('message')}", flush=True)
    if not user_token:
        print("SMOKE_RESULT=FAIL_NO_USER_TOKEN", flush=True)
        return 2

    _, result = request_json(
        "PUT",
        host,
        f"/cn/conference/apps/{app_id}/v1/rooms/{room_uuid}/users/{user_uuid}/entry",
        user_uuid,
        user_token,
        {
            "password": "",
            "platform": 1,
            "role": "participant",
            "streams": [
                {
                    "streamName": "default",
                    "audioState": 1,
                    "videoState": 1,
                    "videoSourceType": 1,
                    "audioSourceType": 1,
                }
            ],
            "userName": f"用户{user_uuid}",
            "version": "3.7.0",
        },
    )
    stream_uuid = result.get("data", {}).get("localUser", {}).get("streamUuid")
    print(f"entry_result_code={result.get('code')} stream_uuid={stream_uuid} message={result.get('message')}", flush=True)
    if not stream_uuid:
        print("SMOKE_RESULT=FAIL_NO_STREAM_UUID", flush=True)
        return 3

    _, result = request_json(
        "PUT",
        host,
        f"/cn/scene/apps/{app_id}/v1/rooms/{room_uuid}/users/{user_uuid}/states/1",
        user_uuid,
        user_token,
    )
    print(f"online_result_code={result.get('code')} message={result.get('message')}", flush=True)

    _, result = request_json(
        "PUT",
        host,
        f"/cn/conference/apps/{app_id}/v1/rooms/{room_uuid}/streams",
        user_uuid,
        user_token,
        {
            "streams": [
                {
                    "audioSourceState": 0,
                    "audioSourceUuid": "1",
                    "audioState": 1,
                    "videoSourceState": 0,
                    "videoSourceUuid": "1",
                    "videoState": 1,
                    "streamUuid": stream_uuid,
                }
            ]
        },
    )
    print(f"update_video_result_code={result.get('code')} message={result.get('message')}", flush=True)
    print("SMOKE_RESULT=SUCCESS", flush=True)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"SMOKE_RESULT=EXCEPTION {exc}", flush=True)
        traceback.print_exc()
        raise
