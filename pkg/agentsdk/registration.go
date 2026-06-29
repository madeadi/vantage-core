package agentsdk

type RegisterResponse struct {
	AgentID  string `json:"agent_id"`
	Token    string `json:"token"`
	GRPCAddr string `json:"grpc_addr"`
}
