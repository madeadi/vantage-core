package action

// MoveOptions holds movement configuration for a navigation action.
//
// Mode:
//   - 0: Free navigation
//   - 1: Strict track mode (stops and waits when obstacle encountered)
//   - 2: Track-priority mode (detours off track when obstacle encountered)
//   - 4: Backward movement
//
// Flags:
//   - precise: Precise-to-point mode, makes the robot stop more accurately at the target
//   - with_yaw: Precise-to-angle mode, yaw field only takes effect when this flag is set
//   - fail_retry_count: Specifies the number of retries after path planning failure; uses default config if not set
//   - find_path_ignoring_dynamic_obstacles: Ignores dynamic obstacles during path planning, suitable for crowded or narrow areas
type MoveOptions struct {
	Mode                int      `json:"mode"`
	Yaw                 float64  `json:"yaw"`
	Flags               []string `json:"flags,omitempty"`
	AcceptablePrecision int      `json:"acceptable_precision"`
	MaxRetryCount       int      `json:"max_retry_count"`
}

// NewMoveOptions creates a MoveOptions with sensible defaults.
func NewMoveOptions() MoveOptions {
	return MoveOptions{
		Mode:                2,
		Yaw:                 0,
		Flags:               []string{"with_yaw", "fail_retry_count"},
		AcceptablePrecision: 1,
		MaxRetryCount:       3,
	}
}
