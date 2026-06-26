package agentskill

import "context"

type Device struct {
	ID      string
	Type    string
	Name    string
	RefID   string
	Context map[string]any
}

type DeviceSkill interface {
	SendToDevice(ctx context.Context, device Device, payload map[string]any) <-chan Result
	GetDeviceStatus(device Device) map[string]any
}
