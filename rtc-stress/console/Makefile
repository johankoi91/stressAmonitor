GO ?= go
NPM ?= npm
VERSION := $(shell node -p "require('./frontend/package.json').version")

.PHONY: build backend frontend test serve dev image netdata sdk-worker
build: backend frontend

backend:
	mkdir -p dist
	cd backend && $(GO) build -trimpath -o ../dist/rtc-stress ./cmd/rtc-stress
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -o ../dist/rtc-stress-linux-amd64 ./cmd/rtc-stress

frontend:
	cd frontend && $(NPM) ci --no-audit --no-fund && $(NPM) run build

test:
	cd backend && $(GO) test -race ./... && $(GO) vet ./...
	cd frontend && $(NPM) run build

serve:
	cd backend && ../dist/rtc-stress serve --data-dir ../.local --frontend ../frontend/dist

dev:
	cd frontend && $(NPM) run dev

netdata:
	./deploy/fetch-netdata.sh

sdk-worker:
	./deploy/fetch-agora-sdk.sh
	docker build --platform linux/amd64 -f deploy/Dockerfile.worker --output type=local,dest=dist .

image: build netdata sdk-worker
	mkdir -p dist/runtime
	cp ../sample_send_h264_pcm ../libagora_rtc_sdk.so ../libaosl.so ../libagora-fdkaac.so dist/runtime/
	cp dist/rtc-stress-linux-amd64 dist/runtime/rtc-stress
	cp dist/rtc-sdk-worker dist/runtime/rtc-sdk-worker
	mkdir -p dist/runtime/sdk
	cp backend/third_party/agora-go/agora_sdk/*.so dist/runtime/sdk/
	mkdir -p dist/runtime/frontend
	cp -R frontend/dist/. dist/runtime/frontend/
	mkdir -p dist/runtime/defaults/audio
	cp deploy/defaults/settings.json dist/runtime/defaults/
	cp res/*.opus dist/runtime/defaults/audio/
	cp .local/netdata/netdata-x86_64-v2.13.0.gz.run deploy/netdata.conf dist/runtime/
	docker build -f deploy/Dockerfile -t rtc-stress-console:$(VERSION) dist/runtime
