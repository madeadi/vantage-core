package action

// Pose represents a 2D pose with optional yaw
type Pose struct {
	X   float64 `json:"x"`
	Y   float64 `json:"y"`
	Yaw float64 `json:"yaw,omitempty"`
}

// Target represents a navigation target
type Target struct {
	Building string `json:"building,omitempty"`
	Floor    string `json:"floor,omitempty"`
	Pose     Pose   `json:"pose"`
}

// MoveToActionOptions holds the full options for a move action
type MoveToActionOptions struct {
	Target      Target      `json:"target"`
	MoveOptions MoveOptions `json:"move_options"`
}

// MoveToAction represents a navigation action
type MoveToAction struct {
	Action
	Options MoveToActionOptions `json:"options"`
}

const moveToActionName = "slamtec.agentsdk.actions.MultiFloorMoveAction"

// NewMoveToAction creates a default MoveToAction
func NewMoveToAction() *MoveToAction {
	return &MoveToAction{
		Action:  Action{ActionName: moveToActionName},
		Options: MoveToActionOptions{},
	}
}

// NewMoveToActionXY creates a MoveToAction targeting (x, y)
func NewMoveToActionXY(x, y float64) *MoveToAction {
	a := NewMoveToAction()
	a.Options.Target.Pose = Pose{X: x, Y: y}
	return a
}

// NewMoveToActionXYYaw creates a MoveToAction targeting (x, y) with a yaw
func NewMoveToActionXYYaw(x, y, yaw float64) *MoveToAction {
	a := NewMoveToActionXY(x, y)
	a.Options.MoveOptions.Yaw = yaw
	a.Options.MoveOptions.Flags = []string{"with_yaw"}
	return a
}

// NewMoveToActionFull creates a MoveToAction with building, floor, pose, and yaw
func NewMoveToActionFull(building, floor string, x, y, yaw float64) *MoveToAction {
	a := NewMoveToAction()
	a.Options.Target = Target{
		Building: building,
		Floor:    floor,
		Pose:     Pose{X: x, Y: y, Yaw: yaw},
	}
	a.Options.MoveOptions.Flags = []string{"with_yaw"}
	return a
}

// poiName is the internal equivalent of Java's static inner PoiName class
type poiName struct {
	PoiName string `json:"poi_name"`
}
