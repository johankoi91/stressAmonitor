module rtcstress

go 1.24

require github.com/gorilla/websocket v1.5.3

require github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2 v2.6.6

replace github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2 => ./third_party/agora-go
