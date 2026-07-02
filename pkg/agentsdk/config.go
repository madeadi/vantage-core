package agentsdk

type BaseConfig struct {
	AgentID string `yaml:"id"`
	Key     string `yaml:"key"`
	Url     string `yaml:"core_url"`
}

type LayoutConfig struct {
	LayoutID string `yaml:"layout_id"`
}

type CameraConfig struct {
	CameraID string     `yaml:"camera_id"`
	Type     CameraType `yaml:"type"`
	Url      string     `yaml:"url"`
}

type CameraType string

const (
	CameraTypeMJpg   CameraType = "mjpg"
	CameraTypeRtsp   CameraType = "rtsp"
	CameraTypeWebRtc CameraType = "webrtc"
)
