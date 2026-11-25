package agoraservice

// #cgo CFLAGS: -I${SRCDIR}/../../agora_sdk/include/c/api2 -I${SRCDIR}/../../agora_sdk/include/c/base
// #include <stdlib.h>
// #include "agora_service.h"
// #include "agora_parameter.h"
import "C"
import (
	"fmt"
	"sync"
	"unsafe"
)

// Callback handles are process-wide and unique in the native SDK. Ownership and
// configuration remain per Service; releasing one never removes another's routes.
var connectionRoutes [4]sync.Map
var encodedAudioRoutes sync.Map
var instanceMu sync.Mutex
var activeInstance *AgoraService

// NewService owns the SDK singleton for a headless RTC audio worker process.
// The original package-level singleton API remains available for existing users.
func NewService(cfg *AgoraServiceConfig) (*AgoraService, error) {
	instanceMu.Lock()
	defer instanceMu.Unlock()
	if activeInstance != nil {
		return nil, fmt.Errorf("SDK 2.6.6 supports one native Service per process")
	}
	if cfg == nil || cfg.AppId == "" {
		return nil, fmt.Errorf("missing service AppId")
	}
	if cfg.EnableVideo || cfg.EnableAudioDevice || cfg.IdleMode || cfg.APMModel != 0 || cfg.EnableLocalAudioTrackWithAPM {
		return nil, fmt.Errorf("instance API currently supports headless audio without APM or idle pooling")
	}
	service := newAgoraService()
	copyConfig := *cfg
	service.serviceConfig = &copyConfig
	service.service = C.agora_service_create()
	if service.service == nil {
		return nil, fmt.Errorf("agora_service_create returned nil")
	}
	native := CAgoraServiceConfig(&copyConfig)
	status := int(C.agora_service_initialize(service.service, native))
	FreeCAgoraServiceConfig(native)
	if status != 0 {
		C.agora_service_release(service.service)
		return nil, fmt.Errorf("agora_service_initialize: %d", status)
	}
	// SDK 4.6.31.135's C initializer only copies positive log levels.
	// Explicitly apply the filter so LOG_LEVEL_NONE (0) does not become INFO.
	if status := int(C.agora_service_set_log_filter(service.service, C.uint(copyConfig.LogLevel))); status != 0 {
		C.agora_service_release(service.service)
		return nil, fmt.Errorf("agora_service_set_log_filter: %d", status)
	}
	service.mediaFactory = newMediaNodeFactoryForService(service)
	if service.mediaFactory == nil {
		C.agora_service_release(service.service)
		return nil, fmt.Errorf("media node factory unavailable")
	}
	service.inited = true
	parameter := service.GetAgoraParameter()
	if status := parameter.SetParameters(`{"rtc.set_app_type":17}`); status < 0 {
		service.mediaFactory.release()
		C.agora_service_release(service.service)
		return nil, fmt.Errorf("app type: %d", status)
	}
	activeInstance = service
	return service, nil
}

func (service *AgoraService) NativeID() string {
	return fmt.Sprintf("%p", service.service)
}
func (service *AgoraService) GetAgoraParameter() *AgoraParameter {
	return &AgoraParameter{cParameter: C.agora_service_get_agora_parameter(service.service)}
}
func (service *AgoraService) Release() int {
	instanceMu.Lock()
	defer instanceMu.Unlock()
	if !service.inited {
		return 0
	}
	active := false
	service.consByCCon.Range(func(_, _ interface{}) bool { active = true; return false })
	if active {
		return -2
	}
	if service.mediaFactory != nil {
		service.mediaFactory.release()
		service.mediaFactory = nil
	}
	result := int(C.agora_service_release(service.service))
	if result == 0 {
		service.service = nil
		service.inited = false
		if activeInstance == service {
			activeInstance = nil
		}
	}
	return result
}

func (conn *RtcConnection) ConfigurePcmBuffer(frames, delayMs int) int {
	if conn.audioTrack == nil || frames < 1 || frames > 100 {
		return -1
	}
	conn.audioTrack.SetMaxBufferedAudioFrameNumber(frames)
	return conn.audioTrack.SetSendDelayMs(delayMs)
}

func (conn *RtcConnection) ServiceID() string {
	if conn.owner == nil {
		return ""
	}
	return conn.owner.NativeID()
}

func registryStore(handle unsafe.Pointer, connection *RtcConnection, kind int) {
	if handle != nil && kind >= 0 && kind < len(connectionRoutes) {
		connectionRoutes[kind].Store(handle, connection)
	}
}
func registryLoad(handle unsafe.Pointer, kind int) *RtcConnection {
	if handle == nil || kind < 0 || kind >= len(connectionRoutes) {
		return nil
	}
	value, ok := connectionRoutes[kind].Load(handle)
	if !ok {
		return nil
	}
	return value.(*RtcConnection)
}
func registryDelete(handle unsafe.Pointer, kind int) {
	if handle != nil && kind >= 0 && kind < len(connectionRoutes) {
		connectionRoutes[kind].Delete(handle)
	}
}
