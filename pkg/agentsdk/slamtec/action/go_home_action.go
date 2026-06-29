package action

type GoHomeActionOption struct {
	Action
	Options map[string]any `json:"options"`
}

func NewGoHomeActionOption() GoHomeActionOption {
	return GoHomeActionOption{
		Action: Action{ActionName: "slamtec.agentsdk.actions.GoHomeAction"},
		Options: map[string]any{
			"gohome_options": map[string]any{
				"back_to_landing":      false,
				"charging_retry_count": 3,
				"flags":                "dock",
				"move_options":         map[string]any{"mode": 2},
			},
		},
	}
}
