package action

type MoveToTagAction struct {
	Action
	Options MoveToTagOptions `json:"options"`
}

type MoveToTagOptions struct {
	Target          TagTarget       `json:"target"`
	MoveToTagConfig moveToTagConfig `json:"move_to_tag_options"`
}

type TagTarget struct {
	X   float64 `json:"x"`
	Y   float64 `json:"y"`
	Yaw float64 `json:"yaw"`
}

type moveToTagConfig struct {
	MoveOptions    tagMoveOptions `json:"move_options"`
	TagType        int            `json:"tag_type"`
	BackwardDocking bool          `json:"backward_docking"`
	ReflectTagNum  int            `json:"reflect_tag_num"`
	DockAllowance  float64        `json:"dock_allowance"`
	ShelvesSize    []ShelfSize    `json:"shelves_size"`
}

type tagMoveOptions struct {
	Mode int `json:"mode"`
}

type ShelfSize struct {
	ShelfColumnarDiameter float64 `json:"shelf_columnar_diameter"`
	ShelfColumnarLength   float64 `json:"shelf_columnar_length"`
	ShelfColumnarWidth    float64 `json:"shelf_columnar_width"`
}

func NewMoveToTagAction(tagX, tagY, tagYaw float64) MoveToTagAction {
	return MoveToTagAction{
		Action: Action{ActionName: "slamtec.agent.actions.MoveToTagAction"},
		Options: MoveToTagOptions{
			Target: TagTarget{X: tagX, Y: tagY, Yaw: tagYaw},
			MoveToTagConfig: moveToTagConfig{
				MoveOptions:     tagMoveOptions{Mode: 0},
				TagType:         3,
				BackwardDocking: true,
				ReflectTagNum:   2,
				DockAllowance:   0.12,
				ShelvesSize:     []ShelfSize{{ShelfColumnarDiameter: 0.8, ShelfColumnarLength: 1.0, ShelfColumnarWidth: 1.5}},
			},
		},
	}
}
