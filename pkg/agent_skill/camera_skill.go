package agentskill

import "context"

type StreamingPayload struct {
	CameraID   string
	Token      string
	WsServer   string
	HttpServer string
}

type CameraStreamingSkill interface {
	StartStreaming(ctx context.Context, payload StreamingPayload) <-chan Result
	StopStreaming(ctx context.Context, cameraID string) <-chan Result
	IsStreamingActive(cameraID string) bool
}

type RtspCamera struct {
	ID              string
	RefID           string
	URL             string
	IsStreamingActive bool
}

type RtspSkill interface {
	StartRtspStreaming(id string)
	StopRtspStreaming(id string)
	ListRtspCameras() []RtspCamera
}

type PtzCameraSkill interface {
	AbsoluteMove(ctx context.Context, pan, tilt, zoom float64) <-chan Result
	ContinuousMove(ctx context.Context, panVel, tiltVel, zoomVel float64) <-chan Result
	StopPtz(ctx context.Context) <-chan Result
	HomePtz(ctx context.Context) <-chan Result
	Zoom(ctx context.Context, zoom float64) <-chan Result
}

type CameraControlSkill interface {
	ZoomCamera(cameraID string, value float32)
	PanCamera(cameraID string, angle float32)
	TiltCamera(cameraID string, angle float32)
}
