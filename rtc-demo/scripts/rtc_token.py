"""Build a demo RTC AccessToken2 from stdin using the existing token library."""
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from token_lib.AccessToken2 import AccessToken, ServiceRtc


def main():
    request = json.load(sys.stdin)
    expire = request.get("expire") or 7200
    uid = request.get("uid") or 0
    token = AccessToken(request["appId"], request["appCertificate"], expire=expire)
    service = ServiceRtc(request["channelName"], uid)
    for privilege in (
        ServiceRtc.kPrivilegeJoinChannel,
        ServiceRtc.kPrivilegePublishAudioStream,
        ServiceRtc.kPrivilegePublishVideoStream,
        ServiceRtc.kPrivilegePublishDataStream,
    ):
        service.add_privilege(privilege, expire)
    token.add_service(service)
    value = token.build()
    if not value:
        raise ValueError("invalid token parameters")
    print(json.dumps({"code": 0, "data": {"token": value}}))


if __name__ == "__main__":
    main()
